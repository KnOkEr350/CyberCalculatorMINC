package backup

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// DirSink раскладывает файлы копии в каталог назначения. Файлы пишутся во
// временный каталог рядом и переименовываются в назначение только после полной
// проверки: неудачное восстановление не оставляет полузаписанного каталога и не
// затрагивает существующий.
type DirSink struct {
	dest   string
	stage  string
	opened []*os.File
}

// NewDirSink готовит восстановление в dest. Каталог назначения не должен
// существовать: поверх живых данных восстановление не идёт.
func NewDirSink(dest string) (*DirSink, error) {
	dest = filepath.Clean(dest)
	if _, err := os.Lstat(dest); err == nil {
		return nil, fmt.Errorf("backup: каталог назначения %s уже существует", dest)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(parent, ".restore-*")
	if err != nil {
		return nil, err
	}
	return &DirSink{dest: dest, stage: stage}, nil
}

func (s *DirSink) Begin(name string) (io.WriteCloser, error) {
	if !ValidEntryPath(name) {
		return nil, fmt.Errorf("%w: %q", ErrPath, name)
	}
	target := filepath.Join(s.stage, filepath.FromSlash(name))
	// Защита от выхода из каталога после всех преобразований пути.
	if rel, err := filepath.Rel(s.stage, target); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("%w: %q", ErrPath, name)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	s.opened = append(s.opened, f)
	return f, nil
}

func (s *DirSink) Abort() {
	for _, f := range s.opened {
		f.Close()
	}
	os.RemoveAll(s.stage)
}

func (s *DirSink) Commit() error {
	for _, f := range s.opened {
		f.Close()
	}
	if err := os.Rename(s.stage, s.dest); err != nil {
		os.RemoveAll(s.stage)
		return err
	}
	return nil
}
