package config

// Redact is how a setting's value may be shown (the admin page, CLI
// output): enough to tell which credential is in use, never enough to
// reveal it. VQ_S3_SECRET_ACCESS_KEY shows "••••••••" and its last 4
// characters, or only "set" when it is shorter than 16; VQ_S3_ACCESS_KEY_ID
// its first 4, "…" and its last 4, or "set" when shorter than 12. Any other
// setting is shown as it is, and an unset value is "". The boot log shows
// neither: it names where each setting came from (Config.Source).
func Redact(key, value string) string {
	if value == "" {
		return ""
	}
	r := []rune(value)
	switch key {
	case "VQ_S3_SECRET_ACCESS_KEY":
		if len(r) < 16 {
			return "set"
		}
		return "••••••••" + string(r[len(r)-4:])
	case "VQ_S3_ACCESS_KEY_ID":
		if len(r) < 12 {
			return "set"
		}
		return string(r[:4]) + "…" + string(r[len(r)-4:])
	}
	return value
}
