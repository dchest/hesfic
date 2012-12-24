package dir

import (
	"encoding/json"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"time"

	"hesfic/block"
)

type Entry struct {
	Name    string
	Size    int64
	ModTime time.Time
	Mode    os.FileMode
	Ref     *block.Ref
}

// Save stores file from disk at the given path and returns its metadata.
func saveFile(path string) (entry *Entry, err error) {
	fi, err := os.Stat(path)
	if err != nil {
		return
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	w := block.NewWriter()
	_, err = io.Copy(w, f)
	if err != nil {
		return
	}
	ref, err := w.Finish()
	if err != nil {
		return
	}
	entry = &Entry{
		Name:    fi.Name(),
		Size:    fi.Size(),
		ModTime: fi.ModTime(),
		Mode:    fi.Mode(),
		Ref:     ref,
	}
	log.Printf("[%d] stored file %s", w.BlockCount(), path)
	return
}

func SaveDirectory(dirpath string) (entry *Entry, err error) {
	fi, err := os.Stat(dirpath)
	if err != nil {
		return
	}
	dir, err := os.Open(dirpath)
	if err != nil {
		return
	}
	defer dir.Close()
	fis, err := dir.Readdir(0)
	if err != nil {
		return
	}
	entries := make([]*Entry, 0)
	// Save files and subdirectories.
	for _, fi := range fis {
		fullpath := filepath.Join(dirpath, fi.Name())
		var e *Entry
		if fi.IsDir() {
			e, err = SaveDirectory(fullpath)
		} else {
			e, err = saveFile(fullpath)
		}
		if err != nil {
			return
		}
		entries = append(entries, e)
	}
	// Save directory index.
	w := block.NewWriter()
	enc := json.NewEncoder(w)
	err = enc.Encode(entries)
	if err != nil {
		return
	}
	ref, err := w.Finish()
	if err != nil {
		return
	}
	log.Printf("[%d] stored directory %s at %s", w.BlockCount(), dirpath, ref)
	entry = &Entry{
		Name:    fi.Name(),
		Size:    fi.Size(),
		ModTime: fi.ModTime(),
		Mode:    fi.Mode(),
		Ref:     ref,
	}
	return
}

func LoadDirectory(ref *block.Ref) (entries []*Entry, err error) {
	r, err := block.NewReader(ref)
	if err != nil {
		return
	}
	err = json.NewDecoder(r).Decode(&entries)
	return
}

func restoreFile(entry *Entry, outdir string) error {
	var path = filepath.Join(outdir, entry.Name)
	if entry.Mode.IsDir() {
		if err := os.MkdirAll(path, entry.Mode); err != nil {
			return err
		}
	} else {
		r, err := block.NewReader(entry.Ref)
		if err != nil {
			return err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, entry.Mode)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, r); err != nil {
			f.Close()
			return err
		}
		if err := f.Sync(); err != nil {
			f.Close()
			os.Remove(path)
			return err
		}
		if err := f.Close(); err != nil {
			os.Remove(path)
			return err
		}
	}
	if err := os.Chtimes(path, time.Now(), entry.ModTime); err != nil {
		return err
	}
	log.Printf("restored %s", path)
	return nil
}

func RestoreDirectory(ref *block.Ref, outdir string) error {
	if err := os.MkdirAll(outdir, 0755); err != nil {
		return err
	}
	entries, err := LoadDirectory(ref)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := restoreFile(e, outdir); err != nil {
			return err
		}
		if e.Mode.IsDir() {
			if err := RestoreDirectory(e.Ref, filepath.Join(outdir, e.Name)); err != nil {
				return err
			}
		}
	}
	return nil
}

func walkDirectory(ref *block.Ref, basePath string, callback func(path string, entry *Entry) error) error {
	entries, err := LoadDirectory(ref)
	if err != nil {
		return err
	}
	for _, e := range entries {
		path := filepath.Join(basePath, e.Name)
		if err := callback(path, e); err != nil {
			return err
		}
		if e.Mode.IsDir() {
			if err := walkDirectory(e.Ref, path, callback); err != nil {
				return err
			}
		}
	}
	return nil
}

func Walk(ref *block.Ref, callback func(path string, entry *Entry) error) error {
	return walkDirectory(ref, "", callback)
}

func verifyFile(entry *Entry) error {
	r, err := block.NewReader(entry.Ref)
	if err != nil {
		return err
	}
	if _, err := io.Copy(ioutil.Discard, r); err != nil {
		return err
	}
	return nil
}

func VerifyDirectory(ref *block.Ref) error {
	return Walk(ref, func(path string, entry *Entry) error {
		if entry.Mode.IsDir() {
			log.Printf("verified directory %s", path)
		} else {
			if err := verifyFile(entry); err != nil {
				return err
			}
			log.Printf("verified file %s", path)
		}
		return nil
	})
}
