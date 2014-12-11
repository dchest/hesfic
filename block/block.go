package block

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/nacl/secretbox"
	"code.google.com/p/snappy-go/snappy"
	"github.com/dchest/blake2b"

	"github.com/dchest/hesfic/config"
)

// Block kinds.
const (
	dataBlockKind    = 0
	pointerBlockKind = 1
)

const (
	nonceSize = 24

	// Size of data header (excluding nonce).
	headerSize = 1 /* kind */ + 4 /* data length */

	minBoxSize = nonceSize + headerSize
)

var (
	// Block size padding multiplier.
	PadSize = 512
)

func init() {
	if PadSize == 0 {
		panic("PadSize is zero")
	}
}

func generateNonce(nonce *[24]byte) (err error) {
	_, err = io.ReadFull(rand.Reader, nonce[:])
	return
}

func readNonce(nonce *[24]byte, p []byte) error {
	if len(p) < len(nonce) {
		return fmt.Errorf("data is too short to contain nonce")
	}
	copy(nonce[:], p)
	return nil
}

type Writer struct {
	h          hash.Hash // hash for refs
	buf        []byte    // buffer for data
	n          int       // number of data bytes in buffer
	refs       []*Ref    // list of block refs
	kind       uint8     // kind of current blocks
	blockCount int       // number of blocks

	box   []byte // temporary buffer for encrypted data
	cdata []byte // temporary buffer for compressed data
}

// newHash returns new BLAKE2b hash keyed with RefHash key.
func newHash() hash.Hash {
	h, err := blake2b.New(&blake2b.Config{
		Size:   RefLen,
		Key:    config.Keys.RefHash[:],
		Person: []byte("hesfic"),
	})
	if err != nil {
		panic(err.Error())
	}
	return h

}

func calculateRef(h hash.Hash, data []byte) *Ref {
	var tmp [RefLen]byte
	h.Reset()
	h.Write(data)
	mac := h.Sum(tmp[:0])
	return RefFromBytes(mac)
}

func NewWriter() *Writer {
	w := new(Writer)
	w.h = newHash()
	w.buf = make([]byte, config.BlockSize)
	w.cdata = make([]byte, headerSize+snappy.MaxEncodedLen(config.BlockSize)+PadSize)
	w.refs = make([]*Ref, 0)
	w.kind = dataBlockKind
	return w
}

func (w *Writer) Write(b []byte) (nn int, err error) {
	nn = len(b)
	left := config.BlockSize - w.n
	if len(b) >= left {
		w.n += copy(w.buf[w.n:], b[:left])
		b = b[left:]
		if err := w.saveBlock(); err != nil {
			return 0, err
		}
	}
	w.n += copy(w.buf[w.n:], b)
	return
}

func (w *Writer) ReadFrom(r io.Reader) (nn int64, err error) {
	for {
		n, err := r.Read(w.buf[w.n:])
		nn += int64(n)
		if err != nil && err != io.EOF {
			return nn, err
		}
		w.n += n
		if w.n == config.BlockSize || err == io.EOF {
			if err := w.saveBlock(); err != nil {
				return nn, err
			}
		}
		if err == io.EOF {
			return nn, nil
		}
	}
	panic("unreachable")
}

func (w *Writer) Finish() (ref *Ref, err error) {
	if w.n > 0 || len(w.refs) == 0 {
		if err := w.saveBlock(); err != nil {
			return nil, err
		}
	}
	ref, err = w.savePointers()
	if err != nil {
		return
	}

	// Reset state.
	w.kind = dataBlockKind
	w.refs = w.refs[:0]
	w.n = 0

	return ref, err
}

func (w *Writer) BlockCount() int {
	return w.blockCount
}

func (w *Writer) saveBlock() error {
	// Calculate hash of uncompressed data for ref.
	ref := calculateRef(w.h, w.buf[:w.n])

	//TODO check if this block exists on disk.
	if blockExistsOnDisk(ref) {
		// Append ref to list.
		w.refs = append(w.refs, ref)
		w.n = 0
		w.blockCount++
		return nil
	}

	// Compress.
	compressedData, err := snappy.Encode(w.cdata[headerSize:], w.buf[:w.n])
	if err != nil {
		return err
	}
	dataLen := headerSize + len(compressedData)

	// Pad with zeroes so that the encrypted box is multiple of PadSize.
	var paddedLen int
	if dataLen == 0 {
		paddedLen = PadSize - nonceSize - secretbox.Overhead
	} else {
		paddedLen = (((dataLen + nonceSize + secretbox.Overhead) + (PadSize - 1)) / PadSize) * PadSize
		paddedLen -= nonceSize + secretbox.Overhead
	}
	for i := dataLen; i < paddedLen; i++ {
		w.cdata[i] = 0
	}
	plainBlock := w.cdata[:paddedLen]

	// Set block kind.
	plainBlock[0] = w.kind
	// Store compressed length.
	binary.BigEndian.PutUint32(plainBlock[1:], uint32(len(compressedData)))

	// Encrypt.
	var nonce [24]byte
	if err := generateNonce(&nonce); err != nil {
		return err
	}
	//TODO avoid allocation
	fullBox := make([]byte, len(nonce)+len(plainBlock)+secretbox.Overhead)
	copy(fullBox, nonce[:])
	secretbox.Seal(fullBox[len(nonce):len(nonce)], plainBlock, &nonce, &config.Keys.BlockEnc)
	// Save to disk.
	if err := writeBlockToDisk(ref, fullBox); err != nil {
		return err
	}
	// Append ref to list.
	w.refs = append(w.refs, ref)
	w.n = 0
	w.blockCount++
	return nil
}

func (w *Writer) savePointers() (ref *Ref, err error) {
	w.kind = pointerBlockKind
	for len(w.refs) > 1 {
		curRefs := w.refs
		w.refs = make([]*Ref, 0)
		for _, v := range curRefs {
			if _, err := w.Write(v.Bytes()); err != nil {
				return nil, err
			}
		}
		if w.n > 0 {
			// Finish last block.
			if err := w.saveBlock(); err != nil {
				return nil, err
			}
		}
	}
	if len(w.refs) == 0 {
		panic("programmer error: w.refs == 0")
	}
	return w.refs[0], nil
}

func blockPath(ref *Ref) string {
	name := ref.String()
	return filepath.Join(config.BlocksPath, name[:2], name[2:])
}

func writeBlockToDisk(ref *Ref, block []byte) error {
	path := blockPath(ref)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0444)
	if err != nil {
		if os.IsExist(err) {
			// Cool, we already have this block.
			// TODO validate that this block is correct?
			return nil
		}
		return err
	}
	if _, err := f.Write(block); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if config.FileSync {
		if err := f.Sync(); err != nil {
			f.Close()
			os.Remove(path)
			return err
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return err
	}
	return nil
}

func blockExistsOnDisk(ref *Ref) bool {
	//TODO verify that the block on disk is correct?
	if _, err := os.Stat(blockPath(ref)); err != nil {
		return false
	}
	return true
}

type Reader struct {
	h     hash.Hash // HMAC for refs
	block []byte    // current block data
	kind  uint8     // current block kind
	refs  []*Ref    // list of block refs

	box   []byte // buffer for nonce + encrypted data
	cdata []byte // buffer for decrypted compressed data
}

func NewReader(ref *Ref) (r *Reader, err error) {
	r = new(Reader)
	r.h = newHash()
	r.box = make([]byte, nonceSize+headerSize+snappy.MaxEncodedLen(config.BlockSize)+PadSize)
	r.refs = []*Ref{ref}
	if err := r.loadPointers(); err != nil {
		return nil, err
	}
	return
}

func (r *Reader) loadPointers() error {
	if err := r.loadBlock(); err != nil {
		return err
	}
	var tmp [RefLen]byte
	for r.kind != dataBlockKind {
		newrefs := make([]*Ref, 0)
		for {
			_, err := r.Read(tmp[:])
			if err != nil {
				if err == io.EOF {
					break
				}
				return err
			}
			newrefs = append(newrefs, RefFromBytes(tmp[:]))
		}
		r.refs = newrefs
		if err := r.loadBlock(); err != nil {
			return err
		}
	}
	return nil
}

func (r *Reader) Read(p []byte) (nn int, err error) {
	for {
		if len(r.block) == 0 {
			if len(r.refs) == 0 {
				err = io.EOF
				return
			}
			// Refill.
			err = r.loadBlock()
			if err != nil {
				return
			}
		}
		n := len(p)
		if n > len(r.block) {
			n = len(r.block)
		}
		copy(p, r.block[:n])
		p = p[n:]
		r.block = r.block[n:]
		nn += n
		if len(p) == 0 {
			return
		}
	}
	panic("unreachable")
}

func (r *Reader) WriteTo(w io.Writer) (nn int64, err error) {
	for {
		var n int
		n, err = w.Write(r.block)
		nn += int64(n)
		if err != nil {
			return
		}
		if len(r.refs) == 0 {
			return
		}
		// Refill.
		err = r.loadBlock()
		if err != nil {
			return
		}
	}
	panic("unreachable")
}

func (r *Reader) loadBlock() error {
	ref := r.refs[0]
	r.refs = r.refs[1:]

	path := blockPath(ref)
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	n, err := f.Read(r.box)
	if err != nil && err != io.EOF {
		return err
	}
	//if err != io.EOF {
	//	//TODO instead we can read the actual block stored disregarding its size.
	//	return fmt.Errorf("didn't read full block: BlockSize is smaller than stored?")
	//}
	if n < minBoxSize {
		return fmt.Errorf("block on disk is too short: %s", ref)
	}
	// Decrypt.
	var nonce [24]byte
	if err := readNonce(&nonce, r.box); err != nil {
		return err
	}
	encryptedBlock := r.box[len(nonce):n]
	decryptedData, ok := secretbox.Open(r.cdata[:0], encryptedBlock, &nonce, &config.Keys.BlockEnc)
	if !ok {
		return fmt.Errorf("failed to decrypt block %s", ref)
	}

	// Load block kind.
	r.kind = decryptedData[0]
	// Load length of compressed data.
	compressedLen := binary.BigEndian.Uint32(decryptedData[1:])

	decryptedData = decryptedData[headerSize : headerSize+compressedLen]

	// Decompress.
	// TODO avoid allocation.
	decompressedData, err := snappy.Decode(nil, decryptedData)
	if err != nil {
		return err
	}

	// Verify hash.
	contentHash := calculateRef(r.h, decompressedData)
	if !ref.Equal(contentHash) {
		return fmt.Errorf("block ref %s doesn't match content %s", ref, contentHash)
	}

	// Set block.
	r.block = decompressedData
	return nil
}

// Walks the given ref and its subrefs.
func WalkRefs(ref *Ref, callback func(*Ref) error) error {
	r := new(Reader)
	r.h = newHash()
	r.box = make([]byte, nonceSize+headerSize+snappy.MaxEncodedLen(config.BlockSize)+PadSize)
	r.refs = []*Ref{ref}
	if err := r.loadBlock(); err != nil {
		return err
	}
	if err := callback(ref); err != nil {
		return err
	}
	var tmp [RefLen]byte
	for r.kind != dataBlockKind {
		newrefs := make([]*Ref, 0)
		for {
			_, err := r.Read(tmp[:])
			if err != nil {
				if err == io.EOF {
					break
				}
				return err
			}
			newrefs = append(newrefs, RefFromBytes(tmp[:]))
		}
		for _, v := range newrefs {
			if err := callback(v); err != nil {
				return err
			}
		}
		r.refs = newrefs
		if err := r.loadBlock(); err != nil {
			return err
		}
	}
	return nil
}
