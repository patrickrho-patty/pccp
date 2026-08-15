package dari

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// VersionMajor is the DARI protocol major version (DARI §9).
const VersionMajor byte = 1

// RecordKind classifies a DARI record (DARI §9).
type RecordKind byte

const (
	KindControl RecordKind = 0
	KindMessage RecordKind = 1
	KindData    RecordKind = 2
	KindAck     RecordKind = 3
	KindReset   RecordKind = 4
	KindReceipt RecordKind = 5
	KindError   RecordKind = 6
	KindPing    RecordKind = 7
)

// String returns the human-readable name of the record kind.
func (k RecordKind) String() string {
	switch k {
	case KindControl:
		return "CONTROL"
	case KindMessage:
		return "MESSAGE"
	case KindData:
		return "DATA"
	case KindAck:
		return "ACK"
	case KindReset:
		return "RESET"
	case KindReceipt:
		return "RECEIPT"
	case KindError:
		return "ERROR"
	case KindPing:
		return "PING"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", byte(k))
	}
}

// Flags is the 16-bit DARI record flags bit field.
type Flags uint16

const (
	FlagCritical   Flags = 1 << 0
	FlagFinal      Flags = 1 << 1
	FlagEncrypted  Flags = 1 << 2
	FlagCompressed Flags = 1 << 3
)

// PrefaceSize is the size of the 32-byte binary prelude.
const PrefaceSize = 32

// MaxHeaderLen is the maximum CBOR header length (DARI §9).
const MaxHeaderLen = 16 * 1024

// MaxPayloadLen is the maximum single DATA payload length (DARI §9).
const MaxPayloadLen = 1024 * 1024

// Record is a DARI record after parsing the 32-byte prelude.
type Record struct {
	VersionMajor byte       `json:"version_major"`
	Kind         RecordKind `json:"kind"`
	Flags        Flags      `json:"flags"`
	MessageType  uint16     `json:"message_type"`
	Header       []byte     `json:"header"`
	Payload      []byte     `json:"payload"`
	LaneID       uint64     `json:"lane_i_d"`
	LaneSequence uint64     `json:"lane_sequence"`
}

// HeaderLen returns the length of the CBOR header.
func (r *Record) HeaderLen() int { return len(r.Header) }

// PayloadLen returns the length of the payload.
func (r *Record) PayloadLen() int { return len(r.Payload) }

// EncodeRecord writes a DARI record (32-byte prelude + header + payload) to w.
func EncodeRecord(w io.Writer, r *Record) error {
	if r.VersionMajor == 0 {
		r.VersionMajor = VersionMajor
	}
	if len(r.Header) > MaxHeaderLen {
		return fmt.Errorf("dari: header exceeds max length %d", MaxHeaderLen)
	}
	if len(r.Payload) > MaxPayloadLen {
		return fmt.Errorf("dari: payload exceeds max length %d", MaxPayloadLen)
	}

	var prelude [PrefaceSize]byte
	prelude[0] = r.VersionMajor
	prelude[1] = byte(r.Kind)
	binary.BigEndian.PutUint16(prelude[2:4], uint16(r.Flags))
	binary.BigEndian.PutUint16(prelude[4:6], r.MessageType)
	binary.BigEndian.PutUint16(prelude[6:8], uint16(len(r.Header)))
	binary.BigEndian.PutUint32(prelude[8:12], uint32(len(r.Payload)))
	binary.BigEndian.PutUint64(prelude[12:20], r.LaneID)
	binary.BigEndian.PutUint64(prelude[20:28], r.LaneSequence)
	// prelude[28:32] reserved, zero

	if _, err := w.Write(prelude[:]); err != nil {
		return fmt.Errorf("dari: write prelude: %w", err)
	}
	if len(r.Header) > 0 {
		if _, err := w.Write(r.Header); err != nil {
			return fmt.Errorf("dari: write header: %w", err)
		}
	}
	if len(r.Payload) > 0 {
		if _, err := w.Write(r.Payload); err != nil {
			return fmt.Errorf("dari: write payload: %w", err)
		}
	}
	return nil
}

// DecodeRecord reads a single DARI record from r.
func DecodeRecord(r io.Reader) (*Record, error) {
	var prelude [PrefaceSize]byte
	if _, err := io.ReadFull(r, prelude[:]); err != nil {
		return nil, fmt.Errorf("dari: read prelude: %w", err)
	}

	rec := &Record{
		VersionMajor: prelude[0],
		Kind:         RecordKind(prelude[1]),
		Flags:        Flags(binary.BigEndian.Uint16(prelude[2:4])),
		MessageType:  binary.BigEndian.Uint16(prelude[4:6]),
	}
	headerLen := binary.BigEndian.Uint16(prelude[6:8])
	payloadLen := binary.BigEndian.Uint32(prelude[8:12])
	rec.LaneID = binary.BigEndian.Uint64(prelude[12:20])
	rec.LaneSequence = binary.BigEndian.Uint64(prelude[20:28])

	reserved := binary.BigEndian.Uint32(prelude[28:32])
	if reserved != 0 {
		return nil, errors.New("dari: non-zero reserved field")
	}
	if rec.VersionMajor != VersionMajor {
		return nil, fmt.Errorf("dari: unsupported version %d", rec.VersionMajor)
	}
	if headerLen > MaxHeaderLen {
		return nil, fmt.Errorf("dari: header length %d exceeds max %d", headerLen, MaxHeaderLen)
	}
	if payloadLen > MaxPayloadLen {
		return nil, fmt.Errorf("dari: payload length %d exceeds max %d", payloadLen, MaxPayloadLen)
	}

	if headerLen > 0 {
		rec.Header = make([]byte, headerLen)
		if _, err := io.ReadFull(r, rec.Header); err != nil {
			return nil, fmt.Errorf("dari: read header: %w", err)
		}
	}
	if payloadLen > 0 {
		rec.Payload = make([]byte, payloadLen)
		if _, err := io.ReadFull(r, rec.Payload); err != nil {
			return nil, fmt.Errorf("dari: read payload: %w", err)
		}
	}
	return rec, nil
}
