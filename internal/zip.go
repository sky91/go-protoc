package internal

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func Unzip(pReader *zip.Reader, dest string) error {
	for _, f := range pReader.File {
		fpath := filepath.Join(dest, f.Name)
		if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("%s: illegal file path", fpath)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(fpath, os.ModePerm); err != nil {
				return fmt.Errorf("os.MkdirAll() error: [%w]", err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return fmt.Errorf("os.MkdirAll() error: [%w]", err)
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return fmt.Errorf("os.OpenFile() error: [%w]", err)
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		_, err = io.Copy(outFile, rc)

		if err = outFile.Close(); err != nil {
			return fmt.Errorf("outFile.Close() error: [%w]", err)
		}
		if err = rc.Close(); err != nil {
			return fmt.Errorf("rc.Close() error: [%w]", err)
		}
	}
	return nil
}
