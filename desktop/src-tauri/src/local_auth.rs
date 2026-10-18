use std::sync::OnceLock;

static TOKEN: OnceLock<Result<String, String>> = OnceLock::new();

pub(crate) fn token() -> Result<&'static str, String> {
    TOKEN.get_or_init(|| {
        let mut bytes = [0u8; 32];
        getrandom::fill(&mut bytes).map_err(|error| format!("local credential generation failed: {error}"))?;
        Ok(bytes.iter().map(|byte| format!("{byte:02x}")).collect())
    }).as_deref().map_err(Clone::clone)
}

pub(crate) fn authorization() -> Result<String, String> {
    Ok(format!("Bearer {}", token()?))
}

#[cfg(test)]
mod tests {
    #[test]
    fn credential_is_stable_within_process_and_sufficiently_long() {
        let first = super::token().unwrap();
        assert_eq!(first.len(), 64);
        assert_eq!(first, super::token().unwrap());
        assert!(first.chars().all(|c| c.is_ascii_hexdigit()));
    }
}
