// Here, Eat Some Files, Imposturous Cloud!
package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"time"

	"github.com/dchest/hesfic/block"
	"github.com/dchest/hesfic/config"
	"github.com/dchest/hesfic/dir"
	"github.com/dchest/hesfic/snapshot"
	"github.com/dchest/hesfic/web"
)

var (
	configFlag  = flag.String("config", "", "config file path")
	keysFlag    = flag.String("keys", "", "key file path")
	commentFlag = flag.String("comment", "", "comment to use when creating snapshot")
	logFlag     = flag.Bool("log", false, "log actions")
	dryRunFlag  = flag.Bool("dry", false, "do not change files")
)

func getConfigDir() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	//TODO on Windows, use a different path.
	return filepath.Join(u.HomeDir, ".hesfic")
}

func fatal(format string, v ...interface{}) {
	if *logFlag {
		log.Fatalf(format, v...)
	}
	fmt.Fprintf(os.Stderr, format+"\n", v...)
	os.Exit(1)
}

func main() {
	flag.Parse()
	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	if !*logFlag {
		log.SetOutput(ioutil.Discard)
	}

	configPath := *configFlag
	keysPath := *keysFlag

	if configPath == "" {
		configPath = filepath.Join(getConfigDir(), "config")
	}
	if keysPath == "" {
		keysPath = filepath.Join(getConfigDir(), "keys")
	}

	if flag.Arg(0) == "genkeys" {
		if err := config.GenerateKeys(keysPath); err != nil {
			fatal("error: %s", err)
		}
		return
	}

	// Load config and keys.
	if err := config.Load(configPath); err != nil {
		fatal("cannot load config: %s", err)
	}
	if err := config.LoadKeys(keysPath); err != nil {
		fatal("cannot load keys: %s", err)
	}

	// Figure out action.
	var err error
	switch flag.Arg(0) {
	case "create":
		err = createSnapshot()
	case "restore":
		err = restoreSnapshot()
	case "verify":
		err = verifySnapshot()
	case "list-snapshots":
		err = listSnapshots()
	case "list-files":
		err = listFiles()
	case "show-ref":
		err = showRef()
	case "gc":
		err = gc()
	case "web":
		err = serveWeb()
	default:
		err = fmt.Errorf("unknown command: %s", flag.Arg(0))
	}
	if err != nil {
		fatal("error: %s", err)
	}
}

func createSnapshot() error {
	if flag.NArg() < 2 || flag.Arg(1) == "" {
		return fmt.Errorf("expecting directory name")
	}
	config.MakePaths()
	dir := flag.Arg(1)
	return snapshot.Create(dir, *commentFlag)
}

func restoreSnapshot() error {
	if flag.NArg() < 3 || flag.Arg(1) == "" || flag.Arg(2) == "" {
		return fmt.Errorf("expecting snapshot name and output directory name")
	}
	snapshotName := flag.Arg(1)
	outDir := flag.Arg(2)
	return snapshot.Restore(outDir, snapshotName)
}

func verifySnapshot() error {
	names, err := getSnapshotNames(1)
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := snapshot.Verify(name); err != nil {
			return err
		}
		log.Printf("snapshot %s OK", name)
	}
	return nil
}

func listSnapshots() error {
	names, err := snapshot.GetNames()
	if err != nil {
		return err
	}
	for _, name := range names {
		si, err := snapshot.LoadInfo(name)
		if err != nil {
			return err
		}

		comment := ""
		if si.Comment != "" {
			comment = "comment:      " + si.Comment + "\n"
		}

		fmt.Printf("snapshot:     %s\ndate:         %s\nsource path:  %s\nroot ref:     %s\n%s\n",
			name, si.Time.Local().Format(time.RFC1123), si.SourcePath, si.DirRef, comment)
	}
	return nil
}

func sizeString(n int64) string {
	const (
		KiB = 1024
		MiB = 1024 * KiB
		GiB = 1024 * MiB
		TiB = 1024 * GiB
	)
	if n >= TiB {
		return fmt.Sprintf("%5.1fT", float64(n)/GiB)
	}
	if n >= GiB {
		return fmt.Sprintf("%5.1fG", float64(n)/GiB)
	}
	if n >= MiB {
		return fmt.Sprintf("%5.1fM", float64(n)/MiB)
	}
	if n >= KiB {
		return fmt.Sprintf("%5.1fK", float64(n)/KiB)
	}
	return fmt.Sprintf("%6d", n)
}

func listDirectory(name string, ref *block.Ref) error {
	files, err := dir.LoadDirectory(ref)
	if err != nil {
		return err
	}
	for _, f := range files {
		fullpath := filepath.Join(name, f.Name)
		fmt.Printf("%s  %s  %s  %s\n", f.Mode, f.ModTime.Local().Format("02 Jan 2006 15:04"),
			sizeString(f.Size), fullpath)
		if f.Mode.IsDir() {
			if err := listDirectory(fullpath, f.Ref); err != nil {
				return err
			}
		}
	}
	return nil
}

func listFiles() error {
	if flag.NArg() < 2 || flag.Arg(1) == "" {
		return fmt.Errorf("expecting snapshot name or directory ref")
	}

	var dirRef *block.Ref
	if snapshot.IsValidName(flag.Arg(1)) {
		// Given snapshot ref, fetch index ref.
		si, err := snapshot.LoadInfo(flag.Arg(1))
		if err != nil {
			return err
		}
		dirRef = si.DirRef
	} else {
		dirRef = block.RefFromHex([]byte(flag.Arg(1)))
		if dirRef == nil {
			return fmt.Errorf("bad ref %q", flag.Arg(1))
		}
	}
	return listDirectory("", dirRef)
}

func showRef() error {
	if flag.NArg() < 2 || flag.Arg(1) == "" {
		return fmt.Errorf("expecting block ref")
	}
	ref := block.RefFromHex([]byte(flag.Arg(1)))
	if ref == nil {
		return fmt.Errorf("bad ref %s", flag.Arg(1))
	}
	r, err := block.NewReader(ref)
	if err != nil {
		return err
	}
	if _, err := io.Copy(os.Stdout, r); err != nil {
		return err
	}
	return nil
}

func getSnapshotNames(argNo int) ([]string, error) {
	if flag.NArg() > argNo {
		// Names of snapshots to leave are given in arguments.
		names := make([]string, 0, flag.NArg()-1)
		for i := argNo; i < flag.NArg(); i++ {
			name := flag.Arg(i)
			if !snapshot.IsValidName(name) {
				return nil, fmt.Errorf("invalid snapshot name %s", name)
			}
			names = append(names, name)
		}
		return names, nil
	}
	// All snapshots.
	return snapshot.GetNames()
}

func gc() (err error) {
	namesToLeave, err := getSnapshotNames(1)
	if err != nil {
		return err
	}
	return snapshot.CollectGarbage(namesToLeave, *dryRunFlag)
}

func serveWeb() (err error) {
	addr := "localhost:0"
	if flag.NArg() > 0 && flag.Arg(1) != "" {
		addr = flag.Arg(1)
	}
	return web.Serve(addr)
}
