package config

import (
	"crypto/rand"
	"fmt"
	"io"
	"io/ioutil"
	"os"
)

// Secret keys.
var Keys struct {
	RefHash     [32]byte // MAC key for refs
	BlockEnc    [32]byte // block encryption key
	SnapshotEnc [32]byte // snapshot encryption key
}

const keysLen = 32 + 32 + 32

func LoadKeys(keysPath string) error {
	data, err := ioutil.ReadFile(keysPath)
	if err != nil {
		return err
	}
	if len(data) != keysLen {
		return fmt.Errorf("wrong key length in %q, must be %d", keysPath, keysLen)
	}
	copy(Keys.RefHash[:], data[0:32])
	copy(Keys.BlockEnc[:], data[32:64])
	copy(Keys.SnapshotEnc[:], data[64:96])
	return nil
}

func GenerateKeys(keysPath string) error {
	var buf [keysLen]byte
	if _, err := io.ReadFull(rand.Reader, buf[:]); err != nil {
		return err
	}
	f, err := os.OpenFile(keysPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0400)
	if err != nil {
		return err
	}
	if _, err := f.Write(buf[:]); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return nil
}
