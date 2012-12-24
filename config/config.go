package config

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
)

const (
	minBlockSize     = 64 * 1024       /* 64 KiB */
	defaultBlockSize = 2 * 1024 * 1024 /* 2 MiB */
)

// Maximum size of block.
var BlockSize int

// Path for blocks.
var BlocksPath string

// Path for snapshots.
var SnapshotsPath string

// Issue fsync call when writing blocks.
var FileSync = false

type serializedConfig struct {
	BlockSize int
	OutPath   string
	FileSync  bool
}

func Load(configPath string) error {
	var sc serializedConfig
	data, err := ioutil.ReadFile(configPath)
	if err != nil {
		return err
	}
	err = json.Unmarshal(data, &sc)
	if err != nil {
		return err
	}
	if sc.BlockSize == 0 {
		BlockSize = defaultBlockSize
	} else if sc.BlockSize < minBlockSize {
		BlockSize = minBlockSize
	} else if sc.BlockSize > 1<<30 {
		return fmt.Errorf("BlockSize must be less than %d", 1<<30)
	} else {
		BlockSize = sc.BlockSize
	}
	FileSync = sc.FileSync
	BlocksPath = filepath.Join(sc.OutPath, "blocks")
	SnapshotsPath = filepath.Join(sc.OutPath, "snapshots")
	return nil
}

func MakePaths() {
	os.MkdirAll(BlocksPath, 0755)
	os.MkdirAll(SnapshotsPath, 0755)
}
