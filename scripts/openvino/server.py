"""Versioned loopback adapter for a pinned OpenVINO GenAI model.

One request owns one pipeline. GPU never falls back to CPU. CPU/GPU, the
runtime versions, template, source revision and all artifact hashes are exposed
for independent verification by the Go provider. No remote code is executed.
"""
import argparse
import hashlib
import importlib.metadata
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import math
from pathlib import Path
import select
import socket
import threading
import time

ADAPTER = "noosphere-openvino-v1"
DEFAULT_OPTIONS = {"temperature": 0.6, "seed": 42, "top_p": 0.95, "top_k": 20,
                   "num_ctx": 4096, "max_output_tokens": 512, "think": False, "stop": []}


def verify_artifacts(root, manifest):
    root = root.resolve()
    aggregate = hashlib.sha256()
    seen = set()
    for item in sorted(manifest["files"], key=lambda row: row["name"]):
        name = item["name"]
        if Path(name).name != name or ":" in name or name in (".", "..") or name.lower() in seen:
            raise ValueError("Invalid or duplicate manifest artifact name")
        seen.add(name.lower())
        path = (root / name).resolve()
        if path.parent != root or not path.is_file():
            raise ValueError("Manifest artifact escapes the model directory or is absent")
        before = path.stat()
        if before.st_size != item["size"]:
            raise ValueError(f"Artifact size differs: {name}")
        h = hashlib.sha256()
        with path.open("rb") as source:
            for block in iter(lambda: source.read(1024 * 1024), b""):
                h.update(block)
        after = path.stat()
        if before.st_mtime_ns != after.st_mtime_ns or before.st_size != after.st_size or h.hexdigest() != item["sha256"]:
            raise ValueError(f"Artifact SHA256 differs or file changed: {name}")
        aggregate.update(f"{name}\0{item['size']}\0{h.hexdigest()}\n".encode())
    return aggregate.hexdigest()


def validate_options(requested, num_ctx, max_output):
    if not isinstance(requested, dict) or set(requested) - set(DEFAULT_OPTIONS):
        raise ValueError("Unsupported option; keep_alive, tools and residency changes are not supported")
    result = dict(DEFAULT_OPTIONS, num_ctx=num_ctx)
    result.update(requested)
    for name, lower, upper in (("temperature", 0, 2), ("top_p", 0.000001, 1)):
        value = result[name]
        if type(value) not in (int, float) or not math.isfinite(value) or not lower <= value <= upper:
            raise ValueError(f"Invalid {name}")
    for name, lower, upper in (("seed", 0, 2**63 - 1), ("top_k", 0, 100000), ("num_ctx", 128, num_ctx), ("max_output_tokens", 1, max_output)):
        if type(result[name]) is not int or not lower <= result[name] <= upper:
            raise ValueError(f"Invalid {name} or configured server limit exceeded")
    if type(result["think"]) is not bool:
        raise ValueError("think must be boolean")
    stops = result["stop"]
    if not isinstance(stops, list) or len(stops) > 16 or any(not isinstance(s, str) or not s or len(s) > 256 for s in stops):
        raise ValueError("Invalid stop strings")
    if result["max_output_tokens"] >= result["num_ctx"]:
        raise ValueError("Output budget leaves no room for the prompt")
    return result


def generation_kwargs(options):
    return {"do_sample": options["temperature"] > 0,
            "temperature": options["temperature"] if options["temperature"] > 0 else 1.0,
            "rng_seed": options["seed"], "top_p": options["top_p"], "top_k": options["top_k"],
            "max_new_tokens": options["max_output_tokens"], "stop_strings": set(options["stop"]),
            "include_stop_str_in_output": False, "apply_chat_template": False,
            "num_return_sequences": 1, "num_beams": 1, "echo": False}


def split_thinking(text, enabled):
    if "</think>" in text:
        thought, answer = text.split("</think>", 1)
        return answer.strip(), thought.removeprefix("<think>").strip()
    if enabled:
        return "", text.removeprefix("<think>").strip()
    return text.strip(), ""


class Adapter:
    def __init__(self, args):
        # Import lazily, so protocol and safety tests need no native runtime/GPU.
        import openvino as ov
        import openvino_genai as genai
        self.genai = genai
        self.args = args
        self.lock = threading.Lock()
        self.manifest = json.loads(args.manifest.read_text(encoding="utf-8-sig"))
        digest = verify_artifacts(args.model_dir, self.manifest)
        core = ov.Core()
        if args.device not in core.available_devices and not (args.device == "GPU.0" and "GPU" in core.available_devices):
            raise ValueError(f"Requested {args.device} is unavailable; no device fallback is allowed")
        device_name = core.get_property(args.device, "FULL_DEVICE_NAME")
        compile_options = {"PERFORMANCE_HINT": "LATENCY"}
        if args.device == "CPU":
            compile_options["INFERENCE_NUM_THREADS"] = args.cpu_threads
        start = time.perf_counter()
        self.pipeline = genai.LLMPipeline(str(args.model_dir.resolve()), args.device, **compile_options)
        self.load_wall_seconds = time.perf_counter() - start
        self.tokenizer = self.pipeline.get_tokenizer()
        defaults = dict(DEFAULT_OPTIONS, num_ctx=args.num_ctx)
        runtime = {"device": args.device, "device_name": device_name,
                   "compile_options": compile_options, "num_ctx": args.num_ctx,
                   "max_output_tokens": args.max_output, "parallel": 1,
                   "default_options": defaults,
                   "context_policy": "prompt_tokens + max_new_tokens <= requested num_ctx",
                   "residency": "externally_managed", "request_timeout_seconds": args.request_timeout}
        self.metadata = {"schema_version": 1, "adapter": ADAPTER, "engine": "openvino_genai",
                         "genai_version": genai.__version__, "openvino_version": ov.__version__,
                         "tokenizers_version": importlib.metadata.version("openvino-tokenizers"),
                         "adapter_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
                         "dependency_versions": {package: importlib.metadata.version(package) for package in ("openvino", "openvino-genai", "openvino-tokenizers", "numpy")},
                         "model_name": self.manifest["model_name"], "model_dir": str(args.model_dir.resolve()),
                         "model_digest": digest, "artifacts": [{k: item[k] for k in ("name", "size", "sha256")} for item in self.manifest["files"]],
                         "chat_template": self.tokenizer.chat_template,
                         "runtime_config": runtime, "source": self.manifest["source"],
                         "model_metadata": {key: self.manifest[key] for key in ("family", "parameter_size", "quantization", "original_model", "license")}}

    def generate(self, body, cancelled):
        if not isinstance(body, dict) or set(body) - {"model", "messages", "options"}:
            raise ValueError("Unsupported native request fields")
        if body.get("model") != self.metadata["model_name"]:
            raise ValueError("Requested model does not match this fixed adapter")
        messages = body.get("messages")
        if not isinstance(messages, list) or not 1 <= len(messages) <= 128:
            raise ValueError("One to 128 text messages are required")
        for message in messages:
            if not isinstance(message, dict) or set(message) != {"role", "content"} or message["role"] not in ("system", "user", "assistant") or not isinstance(message["content"], str):
                raise ValueError("Only system/user/assistant text messages are supported")
        options = validate_options(body.get("options", {}), self.args.num_ctx, self.args.max_output)
        prompt = self.tokenizer.apply_chat_template(messages, True, extra_context={"enable_thinking": options["think"]})
        tokenized = self.tokenizer.encode(prompt, add_special_tokens=False)
        input_count = tokenized.input_ids.shape[1]
        if input_count + options["max_output_tokens"] > options["num_ctx"]:
            raise ValueError("Prompt plus output exceeds the requested context budget")
        config = self.pipeline.get_generation_config()
        for key, value in generation_kwargs(options).items():
            setattr(config, key, value)
        config.validate()
        started = time.monotonic()
        interrupted = False

        def streamer(_text):
            nonlocal interrupted
            interrupted = cancelled() or time.monotonic() - started > self.args.request_timeout
            return self.genai.StreamingStatus.CANCEL if interrupted else self.genai.StreamingStatus.RUNNING

        # A one-element list returns DecodedResults and measured PerfMetrics.
        # A scalar string in this API overload returns only a string.
        result = self.pipeline.generate([prompt], config, streamer=streamer)
        if interrupted:
            raise TimeoutError("Generation cancelled or exceeded the adapter deadline")
        metrics = result.perf_metrics
        count = metrics.get_num_generated_tokens()
        text, thinking = split_thinking(result.texts[0], options["think"])
        measured = {"num_input_tokens": metrics.get_num_input_tokens(), "num_generated_tokens": count,
                    "generate_duration_ms": metrics.get_generate_duration().mean,
                    "inference_duration_ms": metrics.get_inference_duration().mean,
                    "ttft_ms": metrics.get_ttft().mean, "throughput_tokens_per_second": metrics.get_throughput().mean,
                    "note": "GenAI inference duration includes prefill; throughput is the native GenAI metric, not llama.cpp decode throughput."}
        return {"id": f"openvino-{time.time_ns()}", "object": "chat.completion", "created": int(time.time()),
                "engine": "openvino_genai", "model": self.metadata["model_name"], "model_digest": self.metadata["model_digest"],
                "choices": [{"index": 0, "message": {"role": "assistant", "content": text, "reasoning_content": thinking},
                             "finish_reason": "length" if count >= options["max_output_tokens"] else "stop"}],
                "usage": {"prompt_tokens": measured["num_input_tokens"], "completion_tokens": count, "total_tokens": measured["num_input_tokens"] + count},
                "applied_options": options, "openvino_metrics": measured}


class Handler(BaseHTTPRequestHandler):
    server_version = "NoosphereOpenVINO/1"

    def respond(self, status, body):
        raw = json.dumps(body, ensure_ascii=False, allow_nan=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(raw)))
        self.send_header("Cache-Control", "no-store")
        self.send_header("Connection", "close")
        self.end_headers()
        self.wfile.write(raw)

    def do_GET(self):
        adapter = self.server.adapter
        if self.path == "/health":
            self.respond(200, {"status": "ok", "adapter": ADAPTER, "instance": adapter.args.instance, "busy": adapter.lock.locked()})
        elif self.path == "/noosphere/openvino/metadata":
            self.respond(200, adapter.metadata)
        elif self.path == "/v1/models":
            self.respond(200, {"object": "list", "data": [{"id": adapter.metadata["model_name"], "object": "model", "owned_by": "openvino_genai"}]})
        else:
            self.respond(404, {"error": "Unknown endpoint"})

    def disconnected(self):
        try:
            ready, _, _ = select.select([self.connection], [], [], 0)
            return bool(ready) and self.connection.recv(1, socket.MSG_PEEK) == b""
        except OSError:
            return True

    def do_POST(self):
        if self.path not in ("/noosphere/openvino/evaluate", "/v1/chat/completions"):
            self.respond(404, {"error": "Unknown endpoint"})
            return
        if self.headers.get("Origin") or self.headers.get_content_type() != "application/json" or self.headers.get("Transfer-Encoding"):
            self.respond(400, {"error": "Native loopback JSON requests only"})
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
            if not 0 < length <= 1024 * 1024:
                raise ValueError("Request must be between one byte and one MiB")
            self.connection.settimeout(10)
            raw = self.rfile.read(length)
            if len(raw) != length:
                raise ValueError("Incomplete request")
            body = json.loads(raw)
            if self.path == "/v1/chat/completions":
                allowed = {"model", "messages", "stream", "temperature", "seed", "top_p", "top_k", "max_tokens", "stop", "chat_template_kwargs"}
                if not isinstance(body, dict) or set(body) - allowed or body.get("stream", False) is not False:
                    raise ValueError("Only non-streaming text chat is supported")
                template = body.get("chat_template_kwargs", {})
                if not isinstance(template, dict) or set(template) - {"enable_thinking"}:
                    raise ValueError("Unsupported template option")
                options = {key: body[key] for key in ("temperature", "seed", "top_p", "top_k", "stop") if key in body}
                if "max_tokens" in body: options["max_output_tokens"] = body["max_tokens"]
                if "enable_thinking" in template: options["think"] = template["enable_thinking"]
                body = {"model": body.get("model"), "messages": body.get("messages"), "options": options}
            adapter = self.server.adapter
            if not adapter.lock.acquire(blocking=False):
                self.respond(429, {"error": "The single OpenVINO inference slot is busy"})
                return
            try:
                result = adapter.generate(body, self.disconnected)
            finally:
                adapter.lock.release()
            self.respond(200, result)
        except (ValueError, KeyError, TypeError) as exc:
            self.respond(400, {"error": str(exc)[:300]})
        except TimeoutError:
            if not self.disconnected(): self.respond(408, {"error": "Generation cancelled or deadline exceeded"})
        except (BrokenPipeError, ConnectionResetError, ConnectionAbortedError):
            pass
        except Exception as exc:
            # Paths and internal exception data are kept in local stderr only.
            self.log_error("native inference failed: %r", exc)
            if not self.disconnected(): self.respond(500, {"error": "OpenVINO native inference failed; inspect local server log"})


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--model-dir", type=Path, required=True)
    parser.add_argument("--manifest", type=Path, default=Path(__file__).with_name("model.lock.json"))
    parser.add_argument("--device", choices=("GPU", "GPU.0", "CPU"), default="GPU")
    parser.add_argument("--port", type=int, default=18789)
    parser.add_argument("--cpu-threads", type=int, default=2)
    parser.add_argument("--num-ctx", type=int, default=4096)
    parser.add_argument("--max-output", type=int, default=1024)
    parser.add_argument("--request-timeout", type=int, default=120)
    parser.add_argument("--instance", required=True)
    args = parser.parse_args()
    if not 1024 <= args.port <= 65535 or not 1 <= args.cpu_threads <= 16 or not 128 <= args.num_ctx <= 131072 or not 1 <= args.max_output < args.num_ctx or args.max_output > 16384 or not 1 <= args.request_timeout <= 600:
        parser.error("Invalid adapter resource limits")
    # Bind first, so a port conflict cannot compile/load another copy of a model.
    server = ThreadingHTTPServer(("127.0.0.1", args.port), Handler)
    server.daemon_threads = True
    try:
        server.adapter = Adapter(args)
        print(json.dumps({"ready": True, "device": args.device, "model": server.adapter.metadata["model_name"], "load_wall_seconds": server.adapter.load_wall_seconds, "port": args.port}), flush=True)
        server.serve_forever(poll_interval=0.2)
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
