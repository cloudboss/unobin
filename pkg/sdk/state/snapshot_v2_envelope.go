package state

import "github.com/cloudboss/unobin/pkg/sdk/encrypt"

// SealSnapshotV2 encodes and seals a strict version-2 snapshot.
func SealSnapshotV2(snapshot SnapshotV2, enc encrypt.Encrypter) ([]byte, error) {
	body, err := encodeSnapshotV2(snapshot)
	if err != nil {
		return nil, err
	}
	return Seal(body, PayloadTypeState, enc)
}

// OpenSnapshotV2 opens and decodes a strict version-2 snapshot using the
// backend-configured encrypter.
func OpenSnapshotV2(data []byte, enc encrypt.Encrypter) (SnapshotV2, error) {
	body, err := Open(
		data,
		PayloadTypeState,
		func(*Ref) (encrypt.Encrypter, error) {
			return enc, nil
		},
	)
	if err != nil {
		return SnapshotV2{}, err
	}
	return decodeSnapshotV2(body)
}
