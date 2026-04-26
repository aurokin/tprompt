package promptsource

import (
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/hsadler/tprompt/internal/config"
)

func TestResolveTable(t *testing.T) {
	t.Parallel()

	const explicit = "/srv/prompts"
	const xdg = "/var/xdg"
	const home = "/users/jane"

	cases := []struct {
		name    string
		cfg     config.Resolved
		env     map[string]string
		homeDir string
		want    []Source
		wantErr bool
	}{
		{
			name:    "explicit prompts_dir wins over XDG",
			cfg:     config.Resolved{PromptsDir: explicit},
			env:     map[string]string{"XDG_CONFIG_HOME": xdg},
			homeDir: home,
			want: []Source{{
				Path:               explicit,
				Scope:              ScopeGlobal,
				AutoCreateOnAccess: false,
			}},
		},
		{
			name:    "explicit prompts_dir wins with no env",
			cfg:     config.Resolved{PromptsDir: explicit},
			homeDir: "",
			want: []Source{{
				Path:               explicit,
				Scope:              ScopeGlobal,
				AutoCreateOnAccess: false,
			}},
		},
		{
			name:    "default uses XDG when set",
			cfg:     config.Resolved{},
			env:     map[string]string{"XDG_CONFIG_HOME": xdg},
			homeDir: home,
			want: []Source{{
				Path:               filepath.Join(xdg, "tprompt", "prompts"),
				Scope:              ScopeGlobal,
				AutoCreateOnAccess: true,
			}},
		},
		{
			name:    "default falls back to home when XDG unset",
			cfg:     config.Resolved{},
			env:     map[string]string{},
			homeDir: home,
			want: []Source{{
				Path:               filepath.Join(home, ".config", "tprompt", "prompts"),
				Scope:              ScopeGlobal,
				AutoCreateOnAccess: true,
			}},
		},
		{
			name:    "default falls back to home when XDG is whitespace",
			cfg:     config.Resolved{},
			env:     map[string]string{"XDG_CONFIG_HOME": "   "},
			homeDir: home,
			want: []Source{{
				Path:               filepath.Join(home, ".config", "tprompt", "prompts"),
				Scope:              ScopeGlobal,
				AutoCreateOnAccess: true,
			}},
		},
		{
			name:    "default errors when XDG and home are both empty",
			cfg:     config.Resolved{},
			env:     map[string]string{},
			homeDir: "",
			wantErr: true,
		},
		{
			name:    "nil getenv falls back to home",
			cfg:     config.Resolved{},
			env:     nil,
			homeDir: home,
			want: []Source{{
				Path:               filepath.Join(home, ".config", "tprompt", "prompts"),
				Scope:              ScopeGlobal,
				AutoCreateOnAccess: true,
			}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := Resolve(tc.cfg, mapGetenv(tc.env), tc.homeDir)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Resolve: want error, got nil (sources=%v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("Resolve mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// mapGetenv returns a getenv-like func backed by m. A nil m yields a nil
// function so callers can verify Resolve handles a missing env getter.
func mapGetenv(m map[string]string) func(string) string {
	if m == nil {
		return nil
	}
	return func(k string) string { return m[k] }
}
