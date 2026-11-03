package mathtrainer

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const (
	maxScanSize = 1 << 20
	allowPath   = "testdata/privacy_allow.txt"
	selfPath    = "privacy_test.go"
)

// allowList holds bare path-segment names and "<file>:<kind>" entries.
type allowList struct {
	names map[string]bool
	files map[string]bool
}

var (
	pathRE  = regexp.MustCompile(`(/Us` + `ers|/ho` + `me|/Vol` + `umes)/([\w-]+(?:\.[\w-]+)*)`)
	emailRE = regexp.MustCompile(`(?i)[a-z0-9._%+-]+@(?:gmail\.com|googlemail\.com|126\.com|163\.com|qq\.com|foxmail\.com|outlook\.com|hotmail\.com|live\.com|icloud\.com|me\.com|yahoo\.com|sina\.com|aliyun\.com)(?:$|[^a-z0-9-])`)
	hostRE  = regexp.MustCompile(`(?i)(?:[a-z0-9-]+\.)+ts\.net\b`)
	dataExt = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".heic": true, ".gif": true, ".webp": true,
		".pdf": true, ".csv": true, ".xlsx": true, ".sqlite": true, ".db": true, ".parquet": true}
)

// parseAllow reads the allow file; every entry needs a comment on the line above.
func parseAllow(text string) (allowList, error) {
	a := allowList{names: map[string]bool{}, files: map[string]bool{}}
	prevComment := false
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case line == "":
			prevComment = false
		case strings.HasPrefix(line, "#"):
			prevComment = true
		default:
			if !prevComment {
				return a, errors.New("allow file line " + strconv.Itoa(i+1) + ": entry has no comment above it")
			}
			prevComment = false
			if strings.Contains(line, ":") {
				a.files[line] = true
			} else {
				a.names[line] = true
			}
		}
	}
	return a, nil
}

// scanLine returns the kinds of private data found in one line, never the text.
func scanLine(line string, a allowList) []string {
	var kinds []string
	for _, m := range pathRE.FindAllStringSubmatch(line, -1) {
		if !a.names[m[2]] {
			kinds = append(kinds, "path")
			break
		}
	}
	if emailRE.MatchString(line) {
		kinds = append(kinds, "email")
	}
	for _, h := range hostRE.FindAllString(line, -1) {
		h = strings.ToLower(h)
		if h != "example.ts.net" && !strings.HasSuffix(h, ".example.ts.net") {
			kinds = append(kinds, "tailnet")
			break
		}
	}
	return kinds
}

// isDisallowedData reports a data or image file that has no "<file>:data" entry.
func isDisallowedData(file string, a allowList) bool {
	return dataExt[strings.ToLower(filepath.Ext(file))] && !a.files[file+":data"]
}

func TestNoPrivateDataInTrackedFiles(t *testing.T) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	root := strings.TrimSpace(string(out))
	raw, err := os.ReadFile(filepath.Join(root, allowPath))
	if err != nil {
		t.Fatal(err)
	}
	allow, err := parseAllow(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	list, err := exec.Command("git", "-C", root, "ls-files", "-z").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	reportUnlessAllowed := func(file string, line int, kind string) {
		if allow.files[file+":"+kind] {
			return
		}
		t.Errorf("%s:%d: %s", file, line, kind)
	}
	for _, file := range strings.Split(string(list), "\x00") {
		if file == "" || file == selfPath || file == allowPath {
			continue
		}
		if isDisallowedData(file, allow) {
			t.Errorf("%s:0: data", file)
		}
		content, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatal(err)
		}
		head := content
		if len(head) > 8000 {
			head = head[:8000]
		}
		if len(content) > maxScanSize || bytes.IndexByte(head, 0) >= 0 {
			continue
		}
		for i, line := range strings.Split(string(content), "\n") {
			for _, kind := range scanLine(line, allow) {
				reportUnlessAllowed(file, i+1, kind)
			}
		}
	}
}

func TestPrivacyPatterns(t *testing.T) {
	allow := allowList{names: map[string]bool{"me": true}, files: map[string]bool{"docs/a.png:data": true}}
	lineCases := []struct {
		name string
		line string
		want []string
	}{
		{"users path", "/Us" + "ers/alice/proj", []string{"path"}},
		{"home path", "/ho" + "me/bob/x", []string{"path"}},
		{"volumes path", "/Vol" + "umes/data/x", []string{"path"}},
		{"gmail", "someone" + "@gmail.com", []string{"email"}},
		{"126", "someone" + "@126.com", []string{"email"}},
		{"tailnet", "mac.tail1234" + ".ts.net", []string{"tailnet"}},
		{"allowed segment", "/Us" + "ers/me/x", nil},
		{"example domain", "a" + "@example.com", nil},
		{"anthropic", "noreply" + "@anthropic.com", nil},
		{"github noreply", "123+x" + "@users.noreply.github.com", nil},
		{"example tailnet", "desk.example" + ".ts.net", nil},
		{"web route", "/us" + "ers/alice/profile", nil},
		{"two kinds", "/Us" + "ers/alice/x someone" + "@qq.com", []string{"path", "email"}},
	}
	for _, c := range lineCases {
		t.Run(c.name, func(t *testing.T) {
			got := scanLine(c.line, allow)
			if len(got) != len(c.want) {
				t.Fatalf("kinds = %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("kinds = %v, want %v", got, c.want)
				}
			}
		})
	}

	fileCases := []struct {
		name string
		file string
		want bool
	}{
		{"image not allowed", "docs/b.png", true},
		{"image allowed", "docs/a.png", false},
		{"upper-case extension", "x/photo.JPG", true},
		{"database", "math.db", true},
		{"go source", "main.go", false},
	}
	for _, c := range fileCases {
		t.Run(c.name, func(t *testing.T) {
			if got := isDisallowedData(c.file, allow); got != c.want {
				t.Fatalf("isDisallowedData(%q) = %v, want %v", c.file, got, c.want)
			}
		})
	}

	t.Run("allow file needs comments", func(t *testing.T) {
		if _, err := parseAllow("# why\nme\nu\n"); err == nil {
			t.Fatal("entry without a comment above it was accepted")
		}
		a, err := parseAllow("# top\n\n# why\nme\n# why\nf.png:data\n")
		if err != nil {
			t.Fatal(err)
		}
		if !a.names["me"] || !a.files["f.png:data"] {
			t.Fatalf("entries lost: %+v", a)
		}
	})
}

func TestPrivacyHomeDirWithoutTrailingSlash(t *testing.T) {
	allow := allowList{names: map[string]bool{}, files: map[string]bool{}}
	for _, line := range []string{
		"/Us" + "ers/alice",
		`HOME="/ho` + `me/bob"`,
		"cd /Vol" + "umes/data now",
	} {
		got := scanLine(line, allow)
		if len(got) != 1 || got[0] != "path" {
			t.Errorf("case %q: kinds = %v, want [path]", line[:3], got)
		}
	}
}
