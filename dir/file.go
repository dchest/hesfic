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
func saveFile(path string) (file *Entry, err error) {
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
	file = &Entry{
		Name:    fi.Name(),
		Size:    fi.Size(),
		ModTime: fi.ModTime(),
		Mode:    fi.Mode(),
		Ref:     ref,
	}
	log.Printf("[%d] stored file %s", w.BlockCount(), path)
	return
}

func SaveDirectory(dirpath string) (file *Entry, err error) {
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
	files := make([]*Entry, 0)
	// Save files and subdirectories.
	for _, fi := range fis {
		fullpath := filepath.Join(dirpath, fi.Name())
		var f *Entry
		if fi.IsDir() {
			f, err = SaveDirectory(fullpath)
		} else {
			f, err = saveFile(fullpath)
		}
		if err != nil {
			return
		}
		files = append(files, f)
	}
	// Save directory index.
	w := block.NewWriter()
	enc := json.NewEncoder(w)
	err = enc.Encode(files)
	if err != nil {
		return
	}
	ref, err := w.Finish()
	if err != nil {
		return
	}
	log.Printf("[%d] stored directory %s at %s", w.BlockCount(), dirpath, ref)
	file = &Entry{
		Name:    fi.Name(),
		Size:    fi.Size(),
		ModTime: fi.ModTime(),
		Mode:    fi.Mode(),
		Ref:     ref,
	}
	return
}

func LoadDirectory(ref *block.Ref) (files []*Entry, err error) {
	r, err := block.NewReader(ref)
	if err != nil {
		return
	}
	err = json.NewDecoder(r).Decode(&files)
	return
}

func restoreFile(file *Entry, outdir string) error {
	var path = filepath.Join(outdir, file.Name)
	if file.Mode.IsDir() {
		if err := os.MkdirAll(path, file.Mode); err != nil {
			return err
		}
	} else {
		r, err := block.NewReader(file.Ref)
		if err != nil {
			return err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, file.Mode)
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
	if err := os.Chtimes(path, time.Now(), file.ModTime); err != nil {
		return err
	}
	log.Printf("restored %s", path)
	return nil
}

func RestoreDirectory(ref *block.Ref, outdir string) error {
	if err := os.MkdirAll(outdir, 0755); err != nil {
		return err
	}
	files, err := LoadDirectory(ref)
	if err != nil {
		return err
	}
	for _, file := range files {
		if err := restoreFile(file, outdir); err != nil {
			return err
		}
		if file.Mode.IsDir() {
			if err := RestoreDirectory(file.Ref, filepath.Join(outdir, file.Name)); err != nil {
				return err
			}
		}
	}
	return nil
}

func walkDirectory(ref *block.Ref, basePath string, callback func(path string, file *Entry) error) error {
	files, err := LoadDirectory(ref)
	if err != nil {
		return err
	}
	for _, file := range files {
		path := filepath.Join(basePath, file.Name)
		if err := callback(path, file); err != nil {
			return err
		}
		if file.Mode.IsDir() {
			if err := walkDirectory(file.Ref, path, callback); err != nil {
				return err
			}
		}
	}
	return nil
}

func Walk(ref *block.Ref, callback func(path string, file *Entry) error) error {
	return walkDirectory(ref, "", callback)
}

func verifyEntry(file *Entry) error {
	r, err := block.NewReader(file.Ref)
	if err != nil {
		return err
	}
	if _, err := io.Copy(ioutil.Discard, r); err != nil {
		return err
	}
	return nil
}

func VerifyDirectory(ref *block.Ref) error {
	return Walk(ref, func(path string, file *Entry) error {
		if file.Mode.IsDir() {
			log.Printf("verified directory %s", path)
		} else {
			if err := verifyEntry(file); err != nil {
				return err
			}
			log.Printf("verified file %s", path)
		}
		return nil
	})
}
