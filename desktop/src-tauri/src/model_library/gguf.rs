//! Read the GGUF metadata table without reading or hashing tensor payloads.
use std::collections::HashMap;
use std::fs::File;
use std::io::{self, BufReader, Read, Seek};
use std::path::Path;

pub(super) struct Metadata {
    pub name: Option<String>,
    pub architecture: Option<String>,
    pub context: Option<u32>,
    pub shard_count: u16,
    pub embedding: Option<u32>,
    pub projection: Option<u32>,
    pub projector: bool,
    pub has_vision: Option<bool>,
}

fn u32le(r: &mut impl Read) -> io::Result<u32> {
    let mut b = [0; 4];
    r.read_exact(&mut b)?;
    Ok(u32::from_le_bytes(b))
}

fn u64le(r: &mut impl Read) -> io::Result<u64> {
    let mut b = [0; 8];
    r.read_exact(&mut b)?;
    Ok(u64::from_le_bytes(b))
}

const MAX_TABLE: u64 = 256 * 1024 * 1024;

fn string(r: &mut impl Read) -> Result<String, String> {
    let n = u64le(r).map_err(invalid)?;
    if n > 4096 {
        return Err("GGUF 元数据字符串过长".into());
    }
    let mut bytes = vec![0; n as usize];
    r.read_exact(&mut bytes).map_err(invalid)?;
    String::from_utf8(bytes).map_err(invalid)
}

fn invalid(error: impl std::fmt::Display) -> String {
    format!("GGUF 元数据无效：{error}")
}

fn size_of_value(kind: u32) -> Option<u64> {
    match kind {
        0 | 1 | 7 => Some(1),
        2 | 3 => Some(2),
        4..=6 => Some(4),
        10..=12 => Some(8),
        _ => None,
    }
}

fn skip(r: &mut BufReader<File>, kind: u32) -> Result<(), String> {
    let (kind, count) = if kind == 9 {
        (u32le(r).map_err(invalid)?, u64le(r).map_err(invalid)?)
    } else {
        (kind, 1)
    };
    if count > 16_000_000 {
        return Err("GGUF 数组过大".into());
    }
    if kind == 8 {
        for _ in 0..count {
            let len = u64le(r).map_err(invalid)?;
            if len > MAX_TABLE {
                return Err("GGUF 字符串过长".into());
            }
            r.seek_relative(len as i64).map_err(invalid)?;
        }
    } else {
        let size = size_of_value(kind).ok_or("GGUF 元数据类型不受支持")?;
        r.seek_relative((size * count) as i64).map_err(invalid)?;
    }
    Ok(())
}

pub(super) fn read(path: &Path) -> Result<Metadata, String> {
    let f = File::open(path).map_err(invalid)?;
    let file_length = f.metadata().map_err(invalid)?.len();
    let mut r = BufReader::new(f);
    let mut magic = [0; 4];
    r.read_exact(&mut magic).map_err(invalid)?;
    if &magic != b"GGUF" || !matches!(u32le(&mut r).map_err(invalid)?, 2 | 3) {
        return Err("请选择小端 GGUF v2 或 v3 文件".into());
    }
    if u64le(&mut r).map_err(invalid)? == 0 {
        return Err("GGUF 未包含张量".into());
    }
    let entries = u64le(&mut r).map_err(invalid)?;
    if entries > 65_536 {
        return Err("GGUF 元数据项过多".into());
    }
    let mut strings = HashMap::new();
    let mut numbers = HashMap::new();
    let mut vision_projector = false;
    let mut has_vision = None;
    let mut shard = 0;
    let mut shard_count = 1;
    for _ in 0..entries {
        let key = string(&mut r)?;
        let kind = u32le(&mut r).map_err(invalid)?;
        if kind == 8
            && matches!(
                key.as_str(),
                "general.architecture" | "general.type" | "general.name"
            )
        {
            strings.insert(key, string(&mut r)?);
        } else if kind == 4
            && (key.ends_with(".context_length")
                || key.ends_with(".embedding_length")
                || key == "clip.vision.projection_dim")
        {
            numbers.insert(key, u32le(&mut r).map_err(invalid)?);
        } else if key == "clip.has_vision_encoder" && kind == 7 {
            let mut b = [0; 1];
            r.read_exact(&mut b).map_err(invalid)?;
            has_vision = Some(b[0] != 0);
        } else if matches!(key.as_str(), "split.no" | "split.count") && kind == 2 {
            let mut b = [0; 2];
            r.read_exact(&mut b).map_err(invalid)?;
            if key == "split.count" {
                shard_count = u16::from_le_bytes(b);
            } else {
                shard = u16::from_le_bytes(b);
            }
        } else {
            vision_projector |= key == "clip.projector_type";
            skip(&mut r, kind)?;
        }
        if r.stream_position().map_err(invalid)? > MAX_TABLE.min(file_length) {
            return Err("GGUF 元数据表过大或不完整".into());
        }
    }
    if r.stream_position().map_err(invalid)? >= file_length {
        return Err("GGUF 张量数据缺失".into());
    }
    if shard != 0 {
        return Err("分片模型请选择第一片 GGUF".into());
    }
    let architecture = strings.remove("general.architecture");
    let projector = vision_projector
        || architecture.as_deref() == Some("clip")
        || strings.get("general.type").is_some_and(|v| v == "mmproj");
    let get = |key: &str| {
        architecture
            .as_ref()
            .and_then(|arch| numbers.get(&format!("{arch}.{key}")).copied())
    };
    Ok(Metadata {
        shard_count,
        context: get("context_length"),
        embedding: get("embedding_length"),
        projection: numbers.get("clip.vision.projection_dim").copied(),
        architecture,
        name: strings.remove("general.name"),
        projector,
        has_vision,
    })
}
