package snapshot

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/nacl/secretbox"

	"github.com/dchest/hesfic/block"
	"github.com/dchest/hesfic/config"
	"github.com/dchest/hesfic/dir"
)

func IsValidName(name string) bool {
	// E.g. 12d22bc3-e1342b30-a9824648-67400c24-039aa13a-169957c2
	if len(name) != 48+5 {
		return false
	}
	for i := 8; i < len(name); i += 9 {
		if name[i] != '-' {
			return false
		}
	}
	return true
}

func nameToNonce(nonce *[24]byte, name string) error {
	if !IsValidName(name) {
		return fmt.Errorf("invalid snapshot name %s", name)
	}
	b := []byte(strings.Replace(name, "-", "", -1))
	if _, err := hex.Decode(nonce[:], b); err != nil {
		return err
	}
	return nil
}

func nonceToName(n *[24]byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x-%x", n[0:4], n[4:8], n[8:12], n[12:16], n[16:20], n[20:24])
}

// Information about snapshot.
type Info struct {
	Time       time.Time
	Comment    string `json:",omitempty"`
	SourcePath string
	DirRef     *block.Ref
}

func (info *Info) store() (name string, err error) {
	// Marshal.
	data, err := json.Marshal(info)
	if err != nil {
		return
	}

	// Encrypt.
	// Nonce is big endian 8-byte UnixNano timestamp || 16 random bytes.
	var nonce [24]byte

	binary.BigEndian.PutUint64(nonce[:8], uint64(time.Now().UnixNano()))
	if _, err = io.ReadFull(rand.Reader, nonce[8:]); err != nil {
		return
	}
	encryptedData := secretbox.Seal(nil, data, &nonce, &config.Keys.SnapshotEnc)

	// Store.
	name = nonceToName(&nonce)
	path := filepath.Join(config.SnapshotsPath, name)
	err = ioutil.WriteFile(path, encryptedData, 0666)
	return
}

func LoadInfo(name string) (info *Info, err error) {
	if !IsValidName(name) {
		return nil, fmt.Errorf("invalid snapshot name %s", name)
	}
	path := filepath.Join(config.SnapshotsPath, name)
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return
	}

	// Decrypt.
	var nonce [24]byte
	err = nameToNonce(&nonce, name)
	if err != nil {
		return
	}
	decryptedData, ok := secretbox.Open(nil, data, &nonce, &config.Keys.SnapshotEnc)
	if !ok {
		return nil, fmt.Errorf("failed to decrypt snapshot %s", name)
	}
	info = new(Info)
	err = json.Unmarshal(decryptedData, &info)
	return
}

func Create(dirpath string, comment string) error {
	file, err := dir.SaveDirectory(dirpath)
	if err != nil {
		return err
	}
	abspath, err := filepath.Abs(dirpath)
	if err != nil {
		abspath = dirpath
	}
	info := &Info{
		Time:       time.Now(),
		Comment:    comment,
		SourcePath: abspath,
		DirRef:     file.Ref,
	}
	name, err := info.store()
	if err != nil {
		return err
	}
	log.Printf("stored snapshot %s", name)
	return nil
}

func Restore(outdir string, name string) error {
	info, err := LoadInfo(name)
	if err != nil {
		return err
	}
	log.Printf("restoring snapshot %s to %s:", info.DirRef, outdir)
	if err := dir.RestoreDirectory(info.DirRef, outdir); err != nil {
		return err
	}
	return nil
}

func Verify(name string) error {
	info, err := LoadInfo(name)
	if err != nil {
		return err
	}
	if err := dir.VerifyDirectory(info.DirRef); err != nil {
		return err
	}
	return nil
}

func GetNames() (names []string, err error) {
	names = make([]string, 0)
	err = filepath.Walk(config.SnapshotsPath, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return nil // skip directories
		}
		if IsValidName(fi.Name()) {
			names = append(names, fi.Name())
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	return
}
