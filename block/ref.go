package block

import (
	"crypto/subtle"
	"encoding/hex"
	"errors"
)

const RefLen = 24

type Ref [RefLen]byte

func (r0 *Ref) Equal(r1 *Ref) bool {
	return subtle.ConstantTimeCompare(r0[:], r1[:]) == 1
}

func RefFromBytes(b []byte) *Ref {
	if len(b) != RefLen {
		return nil
	}
	var r Ref
	copy(r[:], b[:])
	return &r
}

func RefFromHex(b []byte) *Ref {
	if len(b) != hex.EncodedLen(RefLen) {
		return nil
	}
	var r Ref
	_, err := hex.Decode(r[:], b)
	if err != nil {
		return nil
	}
	return &r
}

func (ref *Ref) Bytes() []byte {
	return ref[:]
}

func (ref *Ref) String() string {
	return hex.EncodeToString(ref[:])
}

func (ref *Ref) MarshalJSON() ([]byte, error) {
	return []byte(`"` + ref.String() + `"`), nil
}

func (ref *Ref) UnmarshalJSON(data []byte) error {
	if len(data) != hex.EncodedLen(RefLen)+2 {
		return errors.New("Ref of wrong length")
	}
	_, err := hex.Decode(ref[:], data[1:len(data)-1])
	return err
}
