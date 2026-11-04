package config

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// This schema-focused TOML reader supports the TOML constructs used by Zacman:
// tables, integers, booleans, quoted strings, comments, and single/multiline
// arrays of strings. Unknown keys and unsupported TOML constructs fail closed.
func parseTOML(data []byte) (Config, error) {
	return parseTOMLMode(data, true, false)
}

func parseInventoryTOML(data []byte) (Config, error) {
	return parseTOMLMode(data, false, true)
}

func parseTOMLMode(data []byte, requireManifest, inventoryOnly bool) (Config, error) {
	if !utf8.Valid(data) {
		return Config{}, fmt.Errorf("input is not valid UTF-8")
	}
	cfg := Default()
	// Referenced inventories are opt-in for existing manifests. Do not silently
	// add default references when decoding older configs without [reference].
	cfg.Reference.PackageFiles = []string{}
	section := ""
	seenTables := map[string]bool{}
	seenKeys := map[string]bool{}
	lines := strings.Split(string(data), "\n")
	var statement strings.Builder
	statementLine := 0
	arrayOpen := false

	for i, raw := range lines {
		lineNo := i + 1
		line := stripTOMLComment(raw)
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !arrayOpen && statement.Len() == 0 && strings.HasPrefix(strings.TrimSpace(line), "[") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasSuffix(trimmed, "]") || strings.HasPrefix(trimmed, "[[") || strings.HasSuffix(trimmed, "]]") {
				return Config{}, fmt.Errorf("line %d: invalid or unsupported table declaration", lineNo)
			}
			name := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			if !validTableName(name) {
				return Config{}, fmt.Errorf("line %d: invalid table name %q", lineNo, name)
			}
			if inventoryOnly && !inventoryTableAllowed(name) {
				return Config{}, fmt.Errorf("line %d: unsupported table [%s] in referenced inventory", lineNo, name)
			}
			if seenTables[name] {
				return Config{}, fmt.Errorf("line %d: duplicate table [%s]", lineNo, name)
			}
			seenTables[name] = true
			section = name
			continue
		}
		if statement.Len() == 0 {
			statementLine = lineNo
		}
		if statement.Len() > 0 {
			statement.WriteByte('\n')
		}
		statement.WriteString(line)
		var err error
		arrayOpen, err = hasOpenArray(statement.String())
		if err != nil {
			return Config{}, fmt.Errorf("line %d: %w", statementLine, err)
		}
		if arrayOpen {
			continue
		}
		if err := parseAssignment(statement.String(), section, &cfg, seenKeys, statementLine, inventoryOnly); err != nil {
			return Config{}, err
		}
		statement.Reset()
	}
	if statement.Len() > 0 || arrayOpen {
		return Config{}, fmt.Errorf("line %d: unterminated value", statementLine)
	}
	if requireManifest {
		for _, required := range []string{"schema_version", "packages.repo", "packages.aur.enabled", "packages.aur.items", "services.enable", "services.disable"} {
			if !seenKeys[required] {
				return Config{}, fmt.Errorf("missing required field %q", required)
			}
		}
	} else if inventoryOnly {
		for _, required := range []string{"schema_version", "packages.repo", "packages.foreign.items"} {
			if !seenKeys[required] {
				return Config{}, fmt.Errorf("referenced inventory is missing required field %q", required)
			}
		}
	}
	return cfg, nil
}

func validTableName(name string) bool {
	switch name {
	case "packages", "packages.aur", "packages.foreign", "services", "mirrors", "pacman", "reference",
		"flatpak", "greeter", "mouse", "desktop", "audio", "kernel", "fonts", "locale",
		"python", "python.uv", "python.pip3", "node", "node.npm", "rust", "rust.cargo", "golang":
		return true
	default:
		return false
	}
}

func inventoryTableAllowed(name string) bool {
	switch name {
	case "packages", "packages.foreign", "kernel", "audio", "desktop", "fonts", "flatpak", "greeter", "mouse", "locale", "python.uv", "python.pip3", "node.npm", "rust.cargo", "golang":
		return true
	default:
		return false
	}
}

func stripTOMLComment(line string) string {
	var quote byte
	escaped := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if quote != 0 {
			if quote == '"' && escaped {
				escaped = false
				continue
			}
			if quote == '"' && c == '\\' {
				escaped = true
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			continue
		}
		if c == '#' {
			return line[:i]
		}
	}
	return line
}

func hasOpenArray(value string) (bool, error) {
	depth := 0
	var quote byte
	escaped := false
	for i := 0; i < len(value); i++ {
		c := value[i]
		if quote != 0 {
			if quote == '"' && escaped {
				escaped = false
				continue
			}
			if quote == '"' && c == '\\' {
				escaped = true
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			continue
		}
		switch c {
		case '[':
			depth++
		case ']':
			depth--
			if depth < 0 {
				return false, fmt.Errorf("unexpected closing bracket")
			}
		}
	}
	if quote != 0 {
		return false, fmt.Errorf("unterminated string")
	}
	if depth > 0 {
		return true, nil
	}
	if depth < 0 {
		return false, fmt.Errorf("unexpected closing bracket")
	}
	return false, nil
}

func parseAssignment(statement, section string, cfg *Config, seen map[string]bool, lineNo int, inventoryOnly bool) error {
	eq := strings.IndexByte(statement, '=')
	if eq < 1 {
		return fmt.Errorf("line %d: expected key = value", lineNo)
	}
	key := strings.TrimSpace(statement[:eq])
	if !validBareKey(key) {
		return fmt.Errorf("line %d: invalid bare key %q", lineNo, key)
	}
	fullKey := key
	if section != "" {
		fullKey = section + "." + key
	}
	if inventoryOnly && !inventoryFieldAllowed(fullKey) {
		return fmt.Errorf("line %d: referenced inventory does not support field %q", lineNo, fullKey)
	}
	if seen[fullKey] {
		return fmt.Errorf("line %d: duplicate key %q", lineNo, fullKey)
	}
	seen[fullKey] = true
	value := strings.TrimSpace(statement[eq+1:])
	switch fullKey {
	case "schema_version":
		decimal := strings.TrimPrefix(strings.TrimPrefix(value, "+"), "-")
		if decimal == "" || (len(decimal) > 1 && decimal[0] == '0') {
			return fmt.Errorf("line %d: schema_version must be a valid decimal TOML integer", lineNo)
		}
		for _, c := range decimal {
			if !(c >= '0' && c <= '9' || c == '_') {
				return fmt.Errorf("line %d: schema_version must be a valid decimal TOML integer", lineNo)
			}
		}
		if strings.HasPrefix(decimal, "_") || strings.HasSuffix(decimal, "_") || strings.Contains(decimal, "__") {
			return fmt.Errorf("line %d: schema_version has invalid digit separators", lineNo)
		}
		n, err := strconv.Atoi(strings.ReplaceAll(value, "_", ""))
		if err != nil {
			return fmt.Errorf("line %d: schema_version must be a valid decimal TOML integer", lineNo)
		}
		cfg.SchemaVersion = n
	case "reference.package_files":
		v, err := parseStringArray(value)
		if err != nil {
			return fmt.Errorf("line %d: reference.package_files: %w", lineNo, err)
		}
		cfg.Reference.PackageFiles = v
	case "packages.repo":
		v, err := parseStringArray(value)
		if err != nil {
			return fmt.Errorf("line %d: packages.repo: %w", lineNo, err)
		}
		cfg.Packages.Repo = v
	case "packages.ignore_updates":
		v, err := parseStringArray(value)
		if err != nil {
			return fmt.Errorf("line %d: packages.ignore_updates: %w", lineNo, err)
		}
		cfg.Packages.IgnoreUpdates = v
	case "packages.foreign.items":
		v, err := parseStringArray(value)
		if err != nil {
			return fmt.Errorf("line %d: packages.foreign.items: %w", lineNo, err)
		}
		cfg.Packages.Foreign.Items = v
	case "packages.aur.enabled":
		if value != "true" && value != "false" {
			return fmt.Errorf("line %d: packages.aur.enabled must be true or false", lineNo)
		}
		cfg.Packages.AUR.Enabled = value == "true"
	case "packages.aur.items":
		v, err := parseStringArray(value)
		if err != nil {
			return fmt.Errorf("line %d: packages.aur.items: %w", lineNo, err)
		}
		cfg.Packages.AUR.Items = v
	case "services.enable":
		v, err := parseStringArray(value)
		if err != nil {
			return fmt.Errorf("line %d: services.enable: %w", lineNo, err)
		}
		cfg.Services.Enable = v
	case "services.disable":
		v, err := parseStringArray(value)
		if err != nil {
			return fmt.Errorf("line %d: services.disable: %w", lineNo, err)
		}
		cfg.Services.Disable = v
	case "mirrors.servers":
		v, err := parseStringArray(value)
		if err != nil {
			return fmt.Errorf("line %d: mirrors.servers: %w", lineNo, err)
		}
		cfg.Mirrors.Servers = v
	case "mirrors.blackarch_servers":
		v, err := parseStringArray(value)
		if err != nil {
			return fmt.Errorf("line %d: mirrors.blackarch_servers: %w", lineNo, err)
		}
		cfg.Mirrors.BlackarchServers = v
	case "pacman.ilove_candy":
		if value != "true" && value != "false" {
			return fmt.Errorf("line %d: pacman.ilove_candy must be true or false", lineNo)
		}
		cfg.Pacman.ILoveCandy = value == "true"
	case "flatpak.pkgs":
		v, err := parseStringArray(value)
		if err != nil {
			return fieldArrayError(lineNo, fullKey, err)
		}
		cfg.Flatpak.Pkgs = v
	case "flatpak.user_pkgs":
		v, err := parseStringArray(value)
		if err != nil {
			return fieldArrayError(lineNo, fullKey, err)
		}
		cfg.Flatpak.UserPkgs = v
	case "flatpak.remote":
		v, err := parseScalarString(value)
		if err != nil {
			return fieldValueError(lineNo, fullKey, err)
		}
		cfg.Flatpak.Remote = v
	case "flatpak.scope":
		v, err := parseScalarString(value)
		if err != nil {
			return fieldValueError(lineNo, fullKey, err)
		}
		cfg.Flatpak.Scope = v
	case "greeter.pkgs":
		v, err := parseStringArray(value)
		if err != nil {
			return fieldArrayError(lineNo, fullKey, err)
		}
		cfg.Greeter.Pkgs = v
	case "greeter.system":
		v, err := parseScalarString(value)
		if err != nil {
			return fieldValueError(lineNo, fullKey, err)
		}
		cfg.Greeter.System = v
	case "greeter.theme":
		v, err := parseScalarString(value)
		if err != nil {
			return fieldValueError(lineNo, fullKey, err)
		}
		cfg.Greeter.Theme = v
	case "greeter.theme_source":
		v, err := parseScalarString(value)
		if err != nil {
			return fieldValueError(lineNo, fullKey, err)
		}
		cfg.Greeter.ThemeSource = v
	case "greeter.theme_package":
		v, err := parseScalarString(value)
		if err != nil {
			return fieldValueError(lineNo, fullKey, err)
		}
		cfg.Greeter.ThemePackage = v
	case "mouse.pkgs":
		v, err := parseStringArray(value)
		if err != nil {
			return fieldArrayError(lineNo, fullKey, err)
		}
		cfg.Mouse.Pkgs = v
	case "mouse.theme":
		v, err := parseScalarString(value)
		if err != nil {
			return fieldValueError(lineNo, fullKey, err)
		}
		cfg.Mouse.Theme = v
	case "mouse.theme_source":
		v, err := parseScalarString(value)
		if err != nil {
			return fieldValueError(lineNo, fullKey, err)
		}
		cfg.Mouse.ThemeSource = v
	case "mouse.theme_package":
		v, err := parseScalarString(value)
		if err != nil {
			return fieldValueError(lineNo, fullKey, err)
		}
		cfg.Mouse.ThemePackage = v
	case "mouse.size":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fieldValueError(lineNo, fullKey, fmt.Errorf("expected integer pixels"))
		}
		cfg.Mouse.Size = n
	case "desktop.pkgs":
		v, err := parseStringArray(value)
		if err != nil {
			return fieldArrayError(lineNo, fullKey, err)
		}
		cfg.Desktop.Pkgs = v
	case "desktop.config":
		v, err := parseStringArray(value)
		if err != nil {
			return fieldArrayError(lineNo, fullKey, err)
		}
		cfg.Desktop.Config = v
	case "audio.pkgs":
		v, err := parseStringArray(value)
		if err != nil {
			return fieldArrayError(lineNo, fullKey, err)
		}
		cfg.Audio.Pkgs = v
	case "audio.config":
		v, err := parseStringArray(value)
		if err != nil {
			return fieldArrayError(lineNo, fullKey, err)
		}
		cfg.Audio.Config = v
	case "kernel.pkgs":
		v, err := parseStringArray(value)
		if err != nil {
			return fieldArrayError(lineNo, fullKey, err)
		}
		cfg.Kernel.Pkgs = v
	case "kernel.config":
		v, err := parseStringArray(value)
		if err != nil {
			return fieldArrayError(lineNo, fullKey, err)
		}
		cfg.Kernel.Config = v
	case "fonts.pkgs":
		v, err := parseStringArray(value)
		if err != nil {
			return fieldArrayError(lineNo, fullKey, err)
		}
		cfg.Fonts.Pkgs = v
	case "fonts.config":
		v, err := parseStringArray(value)
		if err != nil {
			return fieldArrayError(lineNo, fullKey, err)
		}
		cfg.Fonts.Config = v
	case "locale.system_language":
		v, err := parseScalarString(value)
		if err != nil {
			return fieldValueError(lineNo, fullKey, err)
		}
		cfg.Locale.SystemLanguage = v
	case "locale.keyboard_layout":
		v, err := parseScalarString(value)
		if err != nil {
			return fieldValueError(lineNo, fullKey, err)
		}
		cfg.Locale.KeyboardLayout = v
	case "locale.keyboard_variant":
		v, err := parseScalarString(value)
		if err != nil {
			return fieldValueError(lineNo, fullKey, err)
		}
		cfg.Locale.KeyboardVariant = v
	case "locale.keyboard_model":
		v, err := parseScalarString(value)
		if err != nil {
			return fieldValueError(lineNo, fullKey, err)
		}
		cfg.Locale.KeyboardModel = v
	case "locale.keyboard_options":
		v, err := parseScalarString(value)
		if err != nil {
			return fieldValueError(lineNo, fullKey, err)
		}
		cfg.Locale.KeyboardOptions = v
	case "python.uv.pkgs":
		v, err := parseStringArray(value)
		if err != nil {
			return fieldArrayError(lineNo, fullKey, err)
		}
		cfg.Python.UV.Pkgs = v
	case "python.uv.config":
		v, err := parseStringArray(value)
		if err != nil {
			return fieldArrayError(lineNo, fullKey, err)
		}
		cfg.Python.UV.Config = v
	case "python.pip3.pkgs":
		v, err := parseStringArray(value)
		if err != nil {
			return fieldArrayError(lineNo, fullKey, err)
		}
		cfg.Python.Pip3.Pkgs = v
	case "python.pip3.config":
		v, err := parseStringArray(value)
		if err != nil {
			return fieldArrayError(lineNo, fullKey, err)
		}
		cfg.Python.Pip3.Config = v
	case "node.npm.pkgs":
		v, err := parseStringArray(value)
		if err != nil {
			return fieldArrayError(lineNo, fullKey, err)
		}
		cfg.Node.NPM.Pkgs = v
	case "node.npm.config":
		v, err := parseStringArray(value)
		if err != nil {
			return fieldArrayError(lineNo, fullKey, err)
		}
		cfg.Node.NPM.Config = v
	case "rust.cargo.pkgs":
		v, err := parseStringArray(value)
		if err != nil {
			return fieldArrayError(lineNo, fullKey, err)
		}
		cfg.Rust.Cargo.Pkgs = v
	case "rust.cargo.config":
		v, err := parseStringArray(value)
		if err != nil {
			return fieldArrayError(lineNo, fullKey, err)
		}
		cfg.Rust.Cargo.Config = v
	case "golang.pkgs":
		v, err := parseStringArray(value)
		if err != nil {
			return fieldArrayError(lineNo, fullKey, err)
		}
		cfg.Golang.Pkgs = v
	case "golang.config":
		v, err := parseStringArray(value)
		if err != nil {
			return fieldArrayError(lineNo, fullKey, err)
		}
		cfg.Golang.Config = v
	default:
		return fmt.Errorf("line %d: unknown field %q", lineNo, fullKey)
	}
	return nil
}

func inventoryFieldAllowed(field string) bool {
	switch field {
	case "schema_version", "packages.repo", "packages.foreign.items",
		"flatpak.pkgs", "flatpak.user_pkgs", "greeter.pkgs", "greeter.system", "greeter.theme", "greeter.theme_source", "greeter.theme_package",
		"mouse.pkgs", "mouse.theme", "mouse.theme_source", "mouse.theme_package", "mouse.size",
		"desktop.pkgs", "desktop.config", "audio.pkgs", "audio.config", "kernel.pkgs", "kernel.config", "fonts.pkgs", "fonts.config",
		"locale.system_language", "locale.keyboard_layout", "locale.keyboard_variant", "locale.keyboard_model", "locale.keyboard_options",
		"python.uv.pkgs", "python.uv.config", "python.pip3.pkgs", "python.pip3.config", "node.npm.pkgs", "node.npm.config", "rust.cargo.pkgs", "rust.cargo.config", "golang.pkgs", "golang.config":
		return true
	default:
		return false
	}
}

func fieldArrayError(lineNo int, field string, err error) error {
	return fmt.Errorf("line %d: %s: %w", lineNo, field, err)
}
func fieldValueError(lineNo int, field string, err error) error {
	return fmt.Errorf("line %d: %s: %w", lineNo, field, err)
}

func parseScalarString(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) == 0 || (value[0] != '\'' && value[0] != '"') {
		return "", fmt.Errorf("expected a quoted string")
	}
	parsed, next, err := parseTOMLString(value, 0)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value[next:]) != "" {
		return "", fmt.Errorf("unexpected content after string")
	}
	return parsed, nil
}

func validBareKey(key string) bool {
	if key == "" {
		return false
	}
	for _, c := range key {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func parseStringArray(value string) ([]string, error) {
	if len(value) < 2 || value[0] != '[' || value[len(value)-1] != ']' {
		return nil, fmt.Errorf("expected an array of strings")
	}
	body := value[1 : len(value)-1]
	out := []string{}
	i := skipSpace(body, 0)
	if i == len(body) {
		return out, nil
	}
	for {
		if i >= len(body) || (body[i] != '"' && body[i] != '\'') {
			return nil, fmt.Errorf("array values must be quoted strings")
		}
		item, next, err := parseTOMLString(body, i)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
		i = skipSpace(body, next)
		if i == len(body) {
			return out, nil
		}
		if body[i] != ',' {
			return nil, fmt.Errorf("expected comma between array items")
		}
		i = skipSpace(body, i+1)
		if i == len(body) { // TOML permits a trailing comma.
			return out, nil
		}
		if body[i] == ',' {
			return nil, fmt.Errorf("unexpected comma between array items")
		}
	}
}

func skipSpace(value string, i int) int {
	for i < len(value) && (value[i] == ' ' || value[i] == '\t' || value[i] == '\r' || value[i] == '\n') {
		i++
	}
	return i
}

func parseTOMLString(value string, start int) (string, int, error) {
	quote := value[start]
	if quote != '"' && quote != '\'' {
		return "", start, fmt.Errorf("expected quoted string")
	}
	if quote == '\'' {
		for i := start + 1; i < len(value); i++ {
			if value[i] == '\n' || value[i] == '\r' {
				return "", start, fmt.Errorf("literal strings must not span lines")
			}
			if value[i] == '\'' {
				return value[start+1 : i], i + 1, nil
			}
		}
		return "", start, fmt.Errorf("unterminated literal string in array")
	}

	var out strings.Builder
	for i := start + 1; i < len(value); i++ {
		c := value[i]
		if c == '"' {
			return out.String(), i + 1, nil
		}
		if c == '\n' || c == '\r' || (c < 0x20 && c != '\t') {
			return "", start, fmt.Errorf("unescaped control character in basic string")
		}
		if c != '\\' {
			out.WriteByte(c)
			continue
		}
		i++
		if i >= len(value) {
			return "", start, fmt.Errorf("incomplete string escape")
		}
		switch value[i] {
		case 'b':
			out.WriteByte('\b')
		case 't':
			out.WriteByte('\t')
		case 'n':
			out.WriteByte('\n')
		case 'f':
			out.WriteByte('\f')
		case 'r':
			out.WriteByte('\r')
		case '"':
			out.WriteByte('"')
		case '\\':
			out.WriteByte('\\')
		case 'u', 'U':
			digits := 4
			if value[i] == 'U' {
				digits = 8
			}
			if i+digits >= len(value) {
				return "", start, fmt.Errorf("incomplete Unicode escape")
			}
			hex := value[i+1 : i+1+digits]
			code, err := strconv.ParseUint(hex, 16, 32)
			if err != nil || !utf8.ValidRune(rune(code)) {
				return "", start, fmt.Errorf("invalid Unicode escape")
			}
			out.WriteRune(rune(code))
			i += digits
		default:
			return "", start, fmt.Errorf("invalid string escape \\%c", value[i])
		}
	}
	return "", start, fmt.Errorf("unterminated basic string in array")
}

func encodeTOML(cfg Config) ([]byte, error) {
	var out strings.Builder
	fmt.Fprintf(&out, "schema_version = %d\n", cfg.SchemaVersion)
	if len(cfg.Reference.PackageFiles) > 0 {
		out.WriteString("\n[reference]\npackage_files = ")
		writeArray(&out, cfg.Reference.PackageFiles, "  ")
		out.WriteByte('\n')
	}
	out.WriteString("\n[packages]\nrepo = ")
	writeArray(&out, cfg.Packages.Repo, "  ")
	out.WriteString("\nignore_updates = ")
	writeArray(&out, cfg.Packages.IgnoreUpdates, "  ")
	out.WriteString("\n\n[packages.foreign]\nitems = ")
	writeArray(&out, cfg.Packages.Foreign.Items, "  ")
	out.WriteString("\n\n[packages.aur]\n")
	fmt.Fprintf(&out, "enabled = %t\nitems = ", cfg.Packages.AUR.Enabled)
	if len(cfg.Packages.AUR.Items) == 0 {
		out.WriteString("[\n  # Enable the AUR backend to install these names through yay or paru.\n  # If neither helper is installed, Zacman can bootstrap yay from its AUR PKGBUILD.\n  # Review AUR PKGBUILDs: they are third-party build scripts.\n  # \"paru\",\n  # \"yay\",\n]")
	} else {
		writeArray(&out, cfg.Packages.AUR.Items, "  ")
	}
	out.WriteString("\n\n[services]\nenable = ")
	writeArray(&out, cfg.Services.Enable, "  ")
	out.WriteString("\ndisable = ")
	writeArray(&out, cfg.Services.Disable, "  ")
	out.WriteString("\n\n[mirrors]\nservers = ")
	writeArray(&out, cfg.Mirrors.Servers, "  ")
	out.WriteString("\nblackarch_servers = ")
	writeArray(&out, cfg.Mirrors.BlackarchServers, "  ")
	fmt.Fprintf(&out, "\n\n[pacman]\nilove_candy = %t\n", cfg.Pacman.ILoveCandy)
	out.WriteString("\n[flatpak]\npkgs = ")
	writeArray(&out, cfg.Flatpak.Pkgs, "  ")
	out.WriteString("\nuser_pkgs = ")
	writeArray(&out, cfg.Flatpak.UserPkgs, "  ")
	fmt.Fprintf(&out, "\nremote = %s\nscope = %s\n", strconv.Quote(cfg.Flatpak.Remote), strconv.Quote(cfg.Flatpak.Scope))
	out.WriteString("\n[greeter]\npkgs = ")
	writeArray(&out, cfg.Greeter.Pkgs, "  ")
	fmt.Fprintf(&out, "\nsystem = %s\ntheme = %s\ntheme_source = %s\ntheme_package = %s\n", strconv.Quote(cfg.Greeter.System), strconv.Quote(cfg.Greeter.Theme), strconv.Quote(cfg.Greeter.ThemeSource), strconv.Quote(cfg.Greeter.ThemePackage))
	out.WriteString("\n[mouse]\n# Pacman cursor packages are installed separately from selecting the theme.\npkgs = ")
	writeArray(&out, cfg.Mouse.Pkgs, "  ")
	out.WriteString("\n# Use an installed cursor-theme name or an absolute path under a supported icon-theme directory.\n")
	fmt.Fprintf(&out, "theme = %s\n# Set theme_source to local for an installed theme or repo to install theme_package.\ntheme_source = %s\ntheme_package = %s\n# 0 preserves the current cursor size; valid explicit sizes are 8-128 pixels.\nsize = %d\n", strconv.Quote(cfg.Mouse.Theme), strconv.Quote(cfg.Mouse.ThemeSource), strconv.Quote(cfg.Mouse.ThemePackage), cfg.Mouse.Size)
	writeComponentTable(&out, "desktop", cfg.Desktop)
	writeComponentTable(&out, "audio", cfg.Audio)
	writeComponentTable(&out, "kernel", cfg.Kernel)
	writePackageSection(&out, "fonts", cfg.Fonts)
	out.WriteString("\n[locale]\n")
	fmt.Fprintf(&out, "system_language = %s\nkeyboard_layout = %s\nkeyboard_variant = %s\nkeyboard_model = %s\nkeyboard_options = %s\n", strconv.Quote(cfg.Locale.SystemLanguage), strconv.Quote(cfg.Locale.KeyboardLayout), strconv.Quote(cfg.Locale.KeyboardVariant), strconv.Quote(cfg.Locale.KeyboardModel), strconv.Quote(cfg.Locale.KeyboardOptions))
	writePackageSection(&out, "python.uv", cfg.Python.UV)
	writePackageSection(&out, "python.pip3", cfg.Python.Pip3)
	writePackageSection(&out, "node.npm", cfg.Node.NPM)
	writePackageSection(&out, "rust.cargo", cfg.Rust.Cargo)
	writePackageSection(&out, "golang", cfg.Golang)
	return []byte(out.String()), nil
}

func writeComponentTable(out *strings.Builder, name string, section ComponentConfig) {
	writePackageSection(out, name, section)
}

func writePackageSection(out *strings.Builder, name string, section PackageSection) {
	fmt.Fprintf(out, "\n[%s]\npkgs = ", name)
	writeArray(out, section.Pkgs, "  ")
	out.WriteString("\nconfig = ")
	writeArray(out, section.Config, "  ")
	out.WriteByte('\n')
}

func writeArray(out *strings.Builder, values []string, indent string) {
	if len(values) == 0 {
		out.WriteString("[]")
		return
	}
	out.WriteString("[\n")
	for _, value := range values {
		fmt.Fprintf(out, "%s%s,\n", indent, strconv.Quote(value))
	}
	out.WriteByte(']')
}
