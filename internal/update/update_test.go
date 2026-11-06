package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		release, current string
		want             bool
	}{
		{"v0.1.5", "v0.1.4", true},
		{"v0.2.0", "v0.1.9", true},
		{"v0.1.10", "v0.1.9", true},
		{"v0.1.4", "v0.1.4", false},
		{"v0.1.4", "v0.1.5", false},
		{"v0.1.4", "v0.1.4-3-gabc123", false},
		{"v0.1.5", "v0.1.4-3-gabc123", true},
		{"v0.1.5", "dev", false},
		{"nightly", "v0.1.4", false},
	} {
		if got := Newer(c.release, c.current); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.release, c.current, got)
		}
	}
}

func TestExtractAndSwap(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("tar.gz archives are the Linux release")
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range map[string]string{"dbro-v1.0.0-linux-amd64/README.md": "readme", "dbro-v1.0.0-linux-amd64/dbro": "new binary"} {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()

	bin, err := extractBinary(buf.Bytes())
	if err != nil || string(bin) != "new binary" {
		t.Fatalf("extractBinary = %q, %v", bin, err)
	}
	path := filepath.Join(t.TempDir(), "dbro")
	os.WriteFile(path, []byte("old binary"), 0o755)
	if err := swapFile(bin, path); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	st, _ := os.Stat(path)
	if string(got) != "new binary" || st.Mode().Perm()&0o100 == 0 {
		t.Fatalf("after swap: %q %v", got, st.Mode())
	}
	if _, err := os.Stat(path + ".old"); !os.IsNotExist(err) {
		t.Fatal("old binary left behind")
	}
}
