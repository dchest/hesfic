package snapshot

import (
	"log"
	"os"
	"path/filepath"

	"hesfic/block"
	"hesfic/config"
	"hesfic/dir"
)

func CollectGarbage(namesToLeave []string) error {
	if len(namesToLeave) == 0 {
		return nil
	}
	usedRefs := make(map[block.Ref]int)
	for _, name := range namesToLeave {
		info, err := LoadInfo(name)
		if err != nil {
			return err
		}

		// Walk and mark used refs.
		usedRefs[*info.DirRef]++
		err = dir.Walk(info.DirRef, func(path string, file *dir.Entry) error {
			return block.WalkRefs(file.Ref, func(ref *block.Ref) error {
				usedRefs[*ref]++
				return nil
			})
		})
		if err != nil {
			return err
		}
	}

	//for k, v := range usedRefs {
	//	fmt.Printf("%s - %d\n", &k, v)
	//}

	// Remove unused blocks.
	err := filepath.Walk(config.BlocksPath, func(path string, fi os.FileInfo, err error) error {
		if fi.Mode().IsDir() {
			if len(fi.Name()) != 2 && path != config.BlocksPath {
				return filepath.SkipDir // not a block directory, skip
			}
			return nil
		}
		ref := block.RefFromHex([]byte(filepath.Base(filepath.Dir(path)) + fi.Name()))
		if ref == nil {
			return nil // not a block, skip
		}
		if usedRefs[*ref] > 0 {
			return nil // block is used
		}
		// Block unused, remove it.
		log.Printf("removing unused block %s", ref)
		return os.Remove(path)
	})
	if err != nil {
		return err
	}
	return nil
}
