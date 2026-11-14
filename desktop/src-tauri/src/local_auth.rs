pub(crate) fn new_token() -> Result<String, String> {
    let mut bytes = [0u8; 32];
    getrandom::fill(&mut bytes)
        .map_err(|error| format!("local credential generation failed: {error}"))?;
    Ok(bytes.iter().map(|byte| format!("{byte:02x}")).collect())
}

#[cfg(test)]
mod tests {
    #[test]
    fn each_process_receives_a_fresh_credential() {
        let first = super::new_token().unwrap();
        assert_eq!(first.len(), 64);
        assert_ne!(first, super::new_token().unwrap());
        assert!(first.bytes().all(|c| c.is_ascii_hexdigit()));
    }
}
