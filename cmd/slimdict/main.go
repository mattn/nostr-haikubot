// Command slimdict generates a slimmed-down copy of the ipa-neologd dictionary
// for nostr-haikubot.
//
// go-haiku only looks at the POS hierarchy and the 活用形 / 基本形 / 発音
// columns, so 活用型 and 読み are replaced away. That drops about 390MB of
// resident memory: 200MB of strings plus the []string headers of 5.9M rows.
//
// Note that go-haiku skips tokens whose feature slice is shorter than 7, so
// the number of remaining content columns must stay 3 (4 POS columns + 3).
package main

import (
	"archive/zip"
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ikawaha/kagome-dict/dict"
)

const neologdModule = "github.com/ikawaha/kagome-dict-ipa-neologd"

// columns of content.dict, in order. they follow the 4 POS columns.
var columns = []string{
	dict.InflectionalType,   // 活用型   - unused by go-haiku
	dict.InflectionalForm,   // 活用形
	dict.BaseFormIndex,      // 基本形
	dict.ReadingIndex,       // 読み     - unused by go-haiku
	dict.PronunciationIndex, // 発音
}

// keep are the indexes of columns which are kept.
var keep = []int{1, 2, 4}

func main() {
	var in, out string
	flag.StringVar(&in, "in", "", "source dictionary zip (default: the embedded dictionary of "+neologdModule+")")
	flag.StringVar(&out, "out", "neologd-slim.dict", "destination dictionary zip")
	flag.Parse()

	if in == "" {
		f, err := extractModuleDict()
		if err != nil {
			log.Fatal(err)
		}
		defer os.Remove(f)
		in = f
	}
	if err := slim(in, out); err != nil {
		log.Fatal(err)
	}
}

// extractModuleDict concatenates the split dictionary of the ipa-neologd
// module into a single zip file, as the module itself does at runtime.
func extractModuleDict() (string, error) {
	if err := exec.Command("go", "mod", "download", neologdModule).Run(); err != nil {
		return "", fmt.Errorf("go mod download %v: %w", neologdModule, err)
	}
	b, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", neologdModule).Output()
	if err != nil {
		return "", fmt.Errorf("go list -m %v: %w", neologdModule, err)
	}
	names, err := filepath.Glob(filepath.Join(strings.TrimSpace(string(b)), "dict", "*"))
	if err != nil {
		return "", err
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no dictionary found in %v", strings.TrimSpace(string(b)))
	}
	f, err := os.CreateTemp("", "neologd-*.dict")
	if err != nil {
		return "", err
	}
	defer f.Close()
	for _, name := range names {
		r, err := os.Open(name)
		if err != nil {
			return "", err
		}
		_, err = io.Copy(f, r)
		r.Close()
		if err != nil {
			return "", err
		}
	}
	return f.Name(), nil
}

func slim(in, out string) error {
	zr, err := zip.OpenReader(in)
	if err != nil {
		return err
	}
	defer zr.Close()

	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	bw := bufio.NewWriterSize(f, 1<<20)
	zw := zip.NewWriter(bw)

	for _, zf := range zr.File {
		switch zf.Name {
		case dict.ContentDictFileName:
			err = copyContents(zw, zf)
		case dict.ContentMetaFileName:
			err = copyContentsMeta(zw, zf)
		default:
			err = copyRaw(zw, zf)
		}
		if err != nil {
			return fmt.Errorf("%v: %w", zf.Name, err)
		}
	}
	if err = zw.Close(); err != nil {
		return err
	}
	return bw.Flush()
}

// copyContents rewrites content.dict, dropping the columns go-haiku does not
// use. rows are separated by "\n" and columns by "\a", see kagome-dict.
func copyContents(zw *zip.Writer, zf *zip.File) error {
	rc, err := zf.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	w, err := zw.Create(zf.Name)
	if err != nil {
		return err
	}
	br := bufio.NewReaderSize(rc, 1<<20)
	bw := bufio.NewWriterSize(w, 1<<20)
	cols := make([]string, 0, len(keep))
	for n := 0; ; n++ {
		row, err := br.ReadString('\n')
		eof := err == io.EOF
		if err != nil && !eof {
			return err
		}
		row = strings.TrimSuffix(row, "\n")
		if eof && row == "" {
			break
		}
		fields := strings.Split(row, "\a")
		if len(fields) != len(columns) {
			return fmt.Errorf("unexpected number of columns at row %v: %v", n, len(fields))
		}
		cols = cols[:0]
		for _, i := range keep {
			cols = append(cols, fields[i])
		}
		if n > 0 {
			bw.WriteString("\n")
		}
		bw.WriteString(strings.Join(cols, "\a"))
		if eof {
			break
		}
	}
	return bw.Flush()
}

// copyContentsMeta rewrites content.meta so that the indexes point at the
// columns which are left.
func copyContentsMeta(zw *zip.Writer, zf *zip.File) error {
	rc, err := zf.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	meta, err := dict.ReadContentsMeta(rc)
	if err != nil {
		return err
	}
	base := meta[dict.POSHierarchy]
	for i, name := range columns {
		if got, want := meta[name], base+int8(i); got != want {
			return fmt.Errorf("unexpected index of %v: %v, want %v", name, got, want)
		}
	}
	slim := dict.ContentsMeta{
		dict.POSStartIndex: meta[dict.POSStartIndex],
		dict.POSHierarchy:  base,
	}
	for i, k := range keep {
		slim[columns[k]] = base + int8(i)
	}
	w, err := zw.Create(zf.Name)
	if err != nil {
		return err
	}
	_, err = slim.WriteTo(w)
	return err
}

// copyRaw copies a member of the zip without recompressing it.
func copyRaw(zw *zip.Writer, zf *zip.File) error {
	rc, err := zf.OpenRaw()
	if err != nil {
		return err
	}
	header := zf.FileHeader
	w, err := zw.CreateRaw(&header)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, rc)
	return err
}
