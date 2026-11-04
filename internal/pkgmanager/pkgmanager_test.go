package pkgmanager

import "testing"

func TestValidatePackageSpecs(t *testing.T) {
	valid := []struct {
		kind Kind
		name string
	}{
		{Flatpak, "org.gimp.GIMP"},
		{UV, "ruff"},
		{UV, "ruff==0.9.1"},
		{Pip3, "httpx>=0.27"},
		{NPM, "eslint"},
		{NPM, "@types/node"},
		{NPM, "@scope/pkg@1.2.3"},
		{Cargo, "bat"},
		{Cargo, "bat@0.24.0"},
		{Go, "github.com/example/tool@latest"},
	}
	for _, item := range valid {
		if err := ValidatePackage(item.kind, item.name); err != nil {
			t.Errorf("ValidatePackage(%s, %q): %v", item.kind, item.name, err)
		}
	}
	invalid := []struct {
		kind Kind
		name string
	}{
		{NPM, "@/no-scope"},
		{NPM, "@scope/"},
		{UV, "-unsafe-option"},
		{Pip3, "pkg;touch /tmp/nope"},
		{Cargo, "pkg\nother"},
	}
	for _, item := range invalid {
		if err := ValidatePackage(item.kind, item.name); err == nil {
			t.Errorf("ValidatePackage(%s, %q) should fail", item.kind, item.name)
		}
	}
}

func TestIsInstalledMatchesVersionedManagerSpecs(t *testing.T) {
	cases := []struct {
		kind      Kind
		installed []string
		wanted    string
	}{
		{UV, []string{"ruff v0.9.5"}, "ruff==0.9.5"},
		{Pip3, []string{"httpx"}, "httpx>=0.27"},
		{NPM, []string{"eslint"}, "eslint@^9.0.0"},
		{NPM, []string{"@types/node"}, "@types/node@22.0.0"},
		{Cargo, []string{"bat"}, "bat@0.24.0"},
		{Go, []string{"github.com/example/tool@v1.2.3"}, "github.com/example/tool@latest"},
	}
	for _, item := range cases {
		if !IsInstalled(item.kind, item.installed, item.wanted) {
			t.Errorf("IsInstalled(%s, %#v, %q) = false", item.kind, item.installed, item.wanted)
		}
	}
}

func TestParseInstalledManagerInventories(t *testing.T) {
	cases := []struct {
		kind Kind
		out  string
		want []string
	}{
		{Flatpak, "org.gimp.GIMP\ncom.spotify.Client\n", []string{"com.spotify.Client", "org.gimp.GIMP"}},
		{Flatpak, "No applications installed\n", []string{}},
		{UV, "ruff v0.9.1\n - ruff\nblack v24.0\n - black\n", []string{"black", "ruff"}},
		{UV, "No tools installed\n", []string{}},
		{Pip3, `[{"name":"httpx","version":"0.27"},{"name":"ruff","version":"0.9"}]`, []string{"httpx", "ruff"}},
		{NPM, `{"dependencies":{"eslint":{"version":"9.0.0"},"@types/node":{"version":"22.0.0"}}}`, []string{"@types/node", "eslint"}},
		{Cargo, "bat v0.24.0:\n    batcat\nfd v10.0.0:\n    fd\n", []string{"bat", "fd"}},
	}
	for _, tc := range cases {
		got, err := parseInstalled(tc.kind, []byte(tc.out))
		if err != nil {
			t.Errorf("parseInstalled(%s): %v", tc.kind, err)
			continue
		}
		if len(got) != len(tc.want) {
			t.Errorf("parseInstalled(%s) = %#v; want %#v", tc.kind, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("parseInstalled(%s) = %#v; want %#v", tc.kind, got, tc.want)
				break
			}
		}
	}
}

func TestResolveGoInstallPathsKeepsDefaultGOBINBehavior(t *testing.T) {
	gotBin, gotPath := resolveGoInstallPaths("", "/home/hunter/go")
	if gotBin != "/home/hunter/go/bin" || gotPath != "/home/hunter/go" {
		t.Fatalf("resolveGoInstallPaths empty GOBIN = (%q, %q), want (/home/hunter/go/bin, /home/hunter/go)", gotBin, gotPath)
	}
	gotBin, gotPath = resolveGoInstallPaths("/opt/go-bin", "/home/hunter/go")
	if gotBin != "/opt/go-bin" || gotPath != "/home/hunter/go" {
		t.Fatalf("resolveGoInstallPaths explicit GOBIN = (%q, %q)", gotBin, gotPath)
	}
}
