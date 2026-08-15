package dari

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

// evidence.go implements the ordered-evidence commitment (linear chain
// + segmented MMR) and selective disclosure verification (spec F.9),
// plus the multi-party Receipt Attestation (spec F.8).

// DefaultSegmentSize is the dari/1 baseline segment size.
const DefaultSegmentSize = 1024

// eventLeaf computes the F.9 event leaf.
func eventLeaf(sequence uint64, eventDigest Digest) Digest {
	var seq [8]byte
	binary.BigEndian.PutUint64(seq[:], sequence)
	return domainDigest("DARI-EVIDENCE-LEAF-v1\x00", seq[:], eventDigest[:])
}

// emptyLeaf computes the F.9 position-specific empty leaf for segment
// s, position p.
func emptyLeaf(s uint64, p uint32) Digest {
	var sb [8]byte
	var pb [4]byte
	binary.BigEndian.PutUint64(sb[:], s)
	binary.BigEndian.PutUint32(pb[:], p)
	return domainDigest("DARI-EVIDENCE-EMPTY-v1\x00", sb[:], pb[:])
}

// nodeHash combines two child digests.
func nodeHash(left, right Digest) Digest {
	return domainDigest("DARI-EVIDENCE-NODE-v1\x00", left[:], right[:])
}

// segmentLeaf commits one completed/final segment.
func segmentLeaf(s, firstSequence uint64, actualCount uint32, root Digest) Digest {
	var sb [8]byte
	var fb [8]byte
	var cb [4]byte
	binary.BigEndian.PutUint64(sb[:], s)
	binary.BigEndian.PutUint64(fb[:], firstSequence)
	binary.BigEndian.PutUint32(cb[:], actualCount)
	return domainDigest("DARI-EVIDENCE-SEGMENT-v1\x00", sb[:], fb[:], cb[:], root[:])
}

// mmrNode combines equal-height MMR peaks.
func mmrNode(left, right Digest) Digest {
	return domainDigest("DARI-EVIDENCE-MMR-NODE-v1\x00", left[:], right[:])
}

// mmrBag bags peaks right-to-left.
func mmrBag(left, acc Digest) Digest {
	return domainDigest("DARI-EVIDENCE-MMR-BAG-v1\x00", left[:], acc[:])
}

// MMRPeak is one peak: height + digest.
type MMRPeak struct {
	Height uint64
	Digest Digest
}

// EventCommitment is one committed event.
type EventCommitment struct {
	Sequence  uint64
	Type      ObjectType
	Canonical []byte // canonical event body bytes
}

// EventDigest computes the F.9 event digest for one event:
// object_digest(event_type, event_body).
func (e EventCommitment) EventDigest() Digest {
	return KernelObjectDigest(e.Type, e.Canonical)
}

// SegmentedCommitment is the F.9 commitment result for a full event
// list: linear root, MMR peaks, receipt root, segment geometry.
type SegmentedCommitment struct {
	FirstSequence uint64
	LastSequence  uint64
	EventCount    uint64
	SegmentSize   uint32
	SegmentCount  uint64
	LinearRoot    Digest
	Peaks         []MMRPeak // left-to-right range order, strictly descending heights
	MMRRoot       Digest
}

// domainDigest is the single F.9 hasher: domain || parts → Digest.
func domainDigest(domain string, parts ...[]byte) Digest {
	h := sha256.New()
	h.Write([]byte(domain))
	for _, p := range parts {
		h.Write(p)
	}
	var d Digest
	copy(d[:], h.Sum(nil))
	return d
}

// linearChainRoot computes R_N over all events (F.9 linear chain).
func linearChainRoot(exchangeDigest Digest, events []EventCommitment) Digest {
	var seq [8]byte
	r := EvidenceChainStart(exchangeDigest[:])
	for _, ev := range events {
		ed := ev.EventDigest()
		binary.BigEndian.PutUint64(seq[:], ev.Sequence)
		r = domainDigest("DARI-EVIDENCE-EVENT-v1\x00", seq[:], r[:], ed[:])
	}
	return r
}

// BuildSegmentedCommitment computes the full F.9 commitment.
func BuildSegmentedCommitment(exchangeDigest Digest, events []EventCommitment, segmentSize uint32) (*SegmentedCommitment, error) {
	if len(events) == 0 {
		return nil, errors.New("dari: evidence requires at least one event")
	}
	if segmentSize == 0 || segmentSize&(segmentSize-1) != 0 {
		return nil, errors.New("dari: segment size must be a power of two")
	}
	if segmentSize < 16 || segmentSize > 65536 {
		return nil, errors.New("dari: segment size must be a negotiated power of two in [16, 65536]")
	}
	// Contiguity check.
	for i := 1; i < len(events); i++ {
		if events[i].Sequence != events[i-1].Sequence+1 {
			return nil, errors.New("dari: evidence sequences must be contiguous and increasing")
		}
	}
	n := uint64(len(events))
	segCount := (n + uint64(segmentSize) - 1) / uint64(segmentSize)
	if segCount == 0 {
		segCount = 1
	}

	leaves := make([]Digest, 0, n)
	for _, ev := range events {
		ed := ev.EventDigest()
		leaves = append(leaves, eventLeaf(ev.Sequence, ed))
	}

	segLeaves := make([]Digest, 0, segCount)
	for s := uint64(0); s < segCount; s++ {
		start := s * uint64(segmentSize)
		end := start + uint64(segmentSize)
		if end > n {
			end = n
		}
		// Build the segment tree with deterministic empty padding.
		padded := make([]Digest, 0, segmentSize)
		padded = append(padded, leaves[start:end]...)
		for uint32(len(padded)) < segmentSize {
			padded = append(padded, emptyLeaf(s, uint32(len(padded))))
		}
		for len(padded) > 1 {
			next := make([]Digest, 0, len(padded)/2)
			for i := 0; i+1 < len(padded); i += 2 {
				next = append(next, nodeHash(padded[i], padded[i+1]))
			}
			padded = next
		}
		segLeaves = append(segLeaves, segmentLeaf(s, events[start].Sequence, uint32(end-start), padded[0]))
	}

	peaks := appendPeaks(nil, segLeaves)

	// Bag right-to-left: acc = last peak; for each earlier peak (right
	// to left): acc = bag(peak, acc).
	acc := peaks[len(peaks)-1].Digest
	for i := len(peaks) - 2; i >= 0; i-- {
		acc = mmrBag(peaks[i].Digest, acc)
	}
	root := KernelObjectDigestRaw("DARI-EVIDENCE-MMR-ROOT-v1\x00", append(u64be(segCount), acc[:]...))

	return &SegmentedCommitment{
		FirstSequence: events[0].Sequence,
		LastSequence:  events[len(events)-1].Sequence,
		EventCount:    n,
		SegmentSize:   segmentSize,
		SegmentCount:  segCount,
		LinearRoot:    linearChainRoot(exchangeDigest, events),
		Peaks:         peaks,
		MMRRoot:       root,
	}, nil
}

// appendPeaks folds segment leaves into MMR peaks by binary carry and
// returns the canonical peak list (left-to-right, descending heights).
func appendPeaks(peaks []MMRPeak, leaves []Digest) []MMRPeak {
	for _, leaf := range leaves {
		cur := MMRPeak{Height: 0, Digest: leaf}
		for len(peaks) > 0 && peaks[len(peaks)-1].Height == cur.Height {
			prev := peaks[len(peaks)-1]
			peaks = peaks[:len(peaks)-1]
			cur = MMRPeak{Height: prev.Height + 1, Digest: mmrNode(prev.Digest, cur.Digest)}
		}
		peaks = append(peaks, cur)
	}
	return peaks
}

func u64be(v uint64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	return b[:]
}
func u32be(v uint32) []byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	return b[:]
}

// ---------------------------------------------------------------------------
// Selective disclosure verification (F.9).
// ---------------------------------------------------------------------------

// ProofStep is [direction, digest]: 0 = sibling is LEFT, 1 = RIGHT.
type ProofStep struct {
	Direction uint8
	Digest    Digest
}

// EventDisclosure is the F.9 disclosure object.
type EventDisclosure struct {
	EventType        ObjectType
	EventBody        []byte
	Sequence         uint64
	SegmentIndex     uint64
	Position         uint32
	ActualCount      uint32
	SegmentPath      []ProofStep // leaf → segment root
	PeakPath         []ProofStep // segment leaf → peak
	TargetPeakHeight uint64
}

// SelectiveDisclosure is the F.9 selective-disclosure-proof body.
type SelectiveDisclosure struct {
	Version          uint16
	ReceiptDigest    Digest
	SegmentSize      uint32
	SegmentCount     uint64
	Disclosures      []EventDisclosure
	Peaks            []MMRPeak
	OmissionManifest []byte
}

// ErrDisclosureMismatch is the verification failure.
var ErrDisclosureMismatch = errors.New("dari: selective disclosure does not verify")

// VerifySelectiveDisclosure validates disclosures against the receipt
// root per the F.9 algorithm: canonical event, leaf recomputation,
// segment geometry, padding-exact path, peak reconstruction (replace
// exactly one), bagging, root comparison.
func VerifySelectiveDisclosure(sd *SelectiveDisclosure, firstSequence uint64, expectedRoot Digest, expectedOmissionDigest ...Digest) error {
	if sd == nil || len(sd.Disclosures) == 0 || len(sd.Peaks) == 0 {
		return fmt.Errorf("%w: no disclosures or peaks", ErrDisclosureMismatch)
	}
	if sd.SegmentSize == 0 || sd.SegmentSize&(sd.SegmentSize-1) != 0 {
		return fmt.Errorf("%w: segment size not a power of two", ErrDisclosureMismatch)
	}
	if sd.SegmentSize < 16 || sd.SegmentSize > 65536 {
		return fmt.Errorf("%w: segment size outside [16, 65536]", ErrDisclosureMismatch)
	}
	// Peak list shape: strictly descending heights, no duplicates.
	for i := 1; i < len(sd.Peaks); i++ {
		if sd.Peaks[i-1].Height <= sd.Peaks[i].Height {
			return fmt.Errorf("%w: peak heights must strictly descend", ErrDisclosureMismatch)
		}
	}
	// Each disclosure is an independent inclusion proof: its
	// reconstructed peak MUST exactly match one peak in the canonical
	// list; the provided list (not the reconstructions) is bagged to
	// prove the root.
	seenSeq := map[uint64]bool{}
	for i := range sd.Disclosures {
		d := &sd.Disclosures[i]
		if seenSeq[d.Sequence] {
			return fmt.Errorf("%w: duplicated sequence", ErrDisclosureMismatch)
		}
		seenSeq[d.Sequence] = true
		if d.Sequence < firstSequence {
			return fmt.Errorf("%w: disclosed sequence precedes the receipt window", ErrDisclosureMismatch)
		}
		// Geometry implied by the receipt's first sequence.
		off := d.Sequence - firstSequence
		wantSeg := off / uint64(sd.SegmentSize)
		wantPos := uint32(off % uint64(sd.SegmentSize))
		if d.SegmentIndex != wantSeg || d.Position != wantPos {
			return fmt.Errorf("%w: segment geometry mismatch", ErrDisclosureMismatch)
		}
		// Recompute event digest + leaf.
		ed := KernelObjectDigest(d.EventType, d.EventBody)
		leaf := eventLeaf(d.Sequence, ed)
		// Walk the leaf→segment path with exact step count.
		steps := log2(sd.SegmentSize)
		if uint32(len(d.SegmentPath)) != steps {
			return fmt.Errorf("%w: segment path must have exactly %d steps", ErrDisclosureMismatch, steps)
		}
		cur := leaf
		for _, st := range d.SegmentPath {
			if st.Direction == 0 {
				cur = nodeHash(st.Digest, cur)
			} else {
				cur = nodeHash(cur, st.Digest)
			}
		}
		seg := segmentLeaf(d.SegmentIndex, firstSequence+wantSeg*uint64(sd.SegmentSize), d.ActualCount, cur)
		// Walk the segment-leaf → peak path.
		peak := seg
		for _, st := range d.PeakPath {
			if st.Direction == 0 {
				peak = mmrNode(st.Digest, peak)
			} else {
				peak = mmrNode(peak, st.Digest)
			}
		}
		// F.9: exactly `target peak height` MMR steps.
		if uint64(len(d.PeakPath)) != d.TargetPeakHeight {
			return fmt.Errorf("%w: peak path length %d != target height %d", ErrDisclosureMismatch, len(d.PeakPath), d.TargetPeakHeight)
		}
		// The reconstructed peak must exactly match one peak in the
		// canonical list.
		matched := false
		for _, p := range sd.Peaks {
			if p.Height == d.TargetPeakHeight && p.Digest == peak {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%w: reconstructed peak not in the canonical list at height %d", ErrDisclosureMismatch, d.TargetPeakHeight)
		}
	}
	// Bag the canonical peaks right-to-left.
	acc := sd.Peaks[len(sd.Peaks)-1].Digest
	for i := len(sd.Peaks) - 2; i >= 0; i-- {
		acc = mmrBag(sd.Peaks[i].Digest, acc)
	}
	root := KernelObjectDigestRaw("DARI-EVIDENCE-MMR-ROOT-v1\x00", append(u64be(sd.SegmentCount), acc[:]...))
	if root != expectedRoot {
		return fmt.Errorf("%w: root mismatch", ErrDisclosureMismatch)
	}

	// Omission manifest (F.9 label 7): when present, its canonical
	// bytes MUST hash to the receipt's omission-manifest digest under
	// the DARI-OMISSION-MANIFEST-v1 domain, and every range MUST be
	// ordered, non-overlapping, inside the receipt window, and
	// disjoint from the disclosed sequences.
	if len(sd.OmissionManifest) > 0 {
		digest := KernelObjectDigestRaw("DARI-OMISSION-MANIFEST-v1\x00", sd.OmissionManifest)
		if len(expectedOmissionDigest) > 0 && digest != expectedOmissionDigest[0] {
			return fmt.Errorf("%w: omission manifest digest mismatch", ErrDisclosureMismatch)
		}
		if err := validateOmissionManifest(sd.OmissionManifest, sd.SegmentCount*uint64(sd.SegmentSize), firstSequence, seenSeq); err != nil {
			return fmt.Errorf("%w: %v", ErrDisclosureMismatch, err)
		}
	} else if len(expectedOmissionDigest) > 0 && expectedOmissionDigest[0] != (Digest{}) {
		return fmt.Errorf("%w: receipt declares an omission manifest but none was supplied", ErrDisclosureMismatch)
	}
	return nil
}

// OmittedRange is one F.9 omitted-range entry.
type OmittedRange struct {
	StartSeq   uint64 `cbor:"1,keyasint"`
	EndSeq     uint64 `cbor:"2,keyasint"`
	Reason     string `cbor:"3,keyasint"`
	Commitment Digest `cbor:"4,keyasint,omitempty"`
}

// validateOmissionManifest enforces the F.9 rules: canonical array of
// ordered, non-overlapping half-open [start, end) ranges within the
// receipt window, disjoint from disclosed sequences.
func validateOmissionManifest(canonical []byte, eventCapacity uint64, firstSequence uint64, disclosed map[uint64]bool) error {
	var ranges []OmittedRange
	if err := UnmarshalCBOR(canonical, &ranges); err != nil {
		return fmt.Errorf("malformed omission manifest: %w", err)
	}
	var prevEnd uint64
	for i, r := range ranges {
		if r.EndSeq <= r.StartSeq {
			return fmt.Errorf("range %d empty or inverted", i)
		}
		if r.StartSeq < firstSequence || r.EndSeq > firstSequence+eventCapacity {
			return fmt.Errorf("range %d outside receipt bounds", i)
		}
		if i > 0 && r.StartSeq < prevEnd {
			return fmt.Errorf("range %d overlaps its predecessor", i)
		}
		for s := r.StartSeq; s < r.EndSeq; s++ {
			if disclosed[s] {
				return fmt.Errorf("range %d overlaps a disclosed sequence", i)
			}
		}
		prevEnd = r.EndSeq
	}
	return nil
}

func log2(v uint32) uint32 {
	var l uint32
	for v > 1 {
		v >>= 1
		l++
	}
	return l
}

// ---------------------------------------------------------------------------
// Receipt Attestation (F.8).
// ---------------------------------------------------------------------------

// Attestation claim classes.
type AttestationClaimClass uint8

const (
	ClaimDecisionState AttestationClaimClass = 1
	ClaimInferenceIO   AttestationClaimClass = 2
	ClaimEffect        AttestationClaimClass = 3
	ClaimEvidenceRoot  AttestationClaimClass = 4
	ClaimProvenance    AttestationClaimClass = 5
)

// AttestationRole is the F.8 receipt-attestation-role.
type AttestationRole uint8

const (
	AttestRoleRelay     AttestationRole = 1
	AttestRoleInference AttestationRole = 2
	AttestRoleEffect    AttestationRole = 3
)

// AttestationClaim is one claim.
type AttestationClaim struct {
	Class            AttestationClaimClass `cbor:"1,keyasint"`
	Objects          []Digest              `cbor:"2,keyasint"`
	FirstObservedSeq uint64                `cbor:"3,keyasint,omitempty"`
	LastObservedSeq  uint64                `cbor:"4,keyasint,omitempty"`
}

// ReceiptAttestationBody is the F.8 attestation body (0x0703).
type ReceiptAttestationBody struct {
	Version                uint16             `cbor:"1,keyasint"`
	ReceiptBodyDigest      Digest             `cbor:"2,keyasint"`
	SignerCredentialDigest Digest             `cbor:"3,keyasint"`
	Role                   AttestationRole    `cbor:"4,keyasint"`
	Claims                 []AttestationClaim `cbor:"5,keyasint"`
	AtMs                   int64              `cbor:"6,keyasint"`
}

// ReceiptAttestationAAD is the F.8 AAD.
const ReceiptAttestationAAD = "DARI-RECEIPT-ATTESTATION-v1\x00"

// SignReceiptAttestation signs an attestation.
func SignReceiptAttestation(b *ReceiptAttestationBody, priv ed25519.PrivateKey) ([]byte, Digest, error) {
	_, coseBytes, digest, err := SignKernelObject(b, ReceiptAttestationAAD, priv, ObjTypeReceiptAttestation)
	if err != nil {
		return nil, Digest{}, err
	}
	return coseBytes, digest, nil
}

// ErrAttestationScope is the F.8 scope violation.
var ErrAttestationScope = errors.New("ATTESTATION_SCOPE_VIOLATION")

// ValidateAttestationScope enforces role/claim agreement: a signer
// MUST NOT attest another role's observation (relay → decision/state/
// evidence-root/provenance; inference peer → inference IO; effect
// executor → effects).
func ValidateAttestationScope(b *ReceiptAttestationBody) error {
	if b == nil {
		return errors.New("dari: nil attestation")
	}
	for _, c := range b.Claims {
		switch b.Role {
		case AttestRoleRelay:
			if c.Class == ClaimInferenceIO || c.Class == ClaimEffect {
				return fmt.Errorf("%w: relay attested another role's observation", ErrAttestationScope)
			}
		case AttestRoleInference:
			if c.Class != ClaimInferenceIO {
				return fmt.Errorf("%w: inference peer attested a non-inference claim", ErrAttestationScope)
			}
		case AttestRoleEffect:
			if c.Class != ClaimEffect {
				return fmt.Errorf("%w: effect executor attested a non-effect claim", ErrAttestationScope)
			}
		default:
			return errors.New("dari: unknown attestation role")
		}
		// Claims must contain unique object digests.
		seen := map[Digest]bool{}
		for _, d := range c.Objects {
			if seen[d] {
				return errors.New("dari: duplicate object digest in claim")
			}
			seen[d] = true
		}
	}
	return nil
}

// BuildDisclosure constructs the disclosure proof for one committed
// event: the leaf→segment path (with deterministic empty padding) and
// the segment-leaf→peak path. Prover side of F.9.
func BuildDisclosure(events []EventCommitment, segmentSize uint32, sequence uint64) (*EventDisclosure, error) {
	if len(events) == 0 {
		return nil, errors.New("dari: no events")
	}
	// Locate the event.
	idx := -1
	for i, ev := range events {
		if ev.Sequence == sequence {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, errors.New("dari: sequence not committed")
	}
	off := uint64(idx)
	segIndex := off / uint64(segmentSize)
	pos := uint32(off % uint64(segmentSize))
	firstSequence := events[0].Sequence

	// Leaves for the containing segment with padding.
	segStart := int(segIndex) * int(segmentSize)
	segEnd := segStart + int(segmentSize)
	if segEnd > len(events) {
		segEnd = len(events)
	}
	leaves := make([]Digest, 0, segmentSize)
	for _, ev := range events[segStart:segEnd] {
		ed := ev.EventDigest()
		leaves = append(leaves, eventLeaf(ev.Sequence, ed))
	}
	actualCount := uint32(segEnd - segStart)
	for uint32(len(leaves)) < segmentSize {
		leaves = append(leaves, emptyLeaf(segIndex, uint32(len(leaves))))
	}
	// Merkle path for pos.
	segPath, segRoot := merklePath(leaves, pos)
	_ = segRoot

	// MMR peaks + path from the segment leaf.
	segLeaves := make([]Digest, 0)
	for s := uint64(0); uint64(int(s)+1) <= (uint64(len(events))+uint64(segmentSize)-1)/uint64(segmentSize); s++ {
		ss := int(s) * int(segmentSize)
		se := ss + int(segmentSize)
		if se > len(events) {
			se = len(events)
		}
		l2 := make([]Digest, 0, segmentSize)
		for _, ev := range events[ss:se] {
			ed := ev.EventDigest()
			l2 = append(l2, eventLeaf(ev.Sequence, ed))
		}
		ac := uint32(se - ss)
		for uint32(len(l2)) < segmentSize {
			l2 = append(l2, emptyLeaf(s, uint32(len(l2))))
		}
		for len(l2) > 1 {
			next := make([]Digest, 0, len(l2)/2)
			for i := 0; i+1 < len(l2); i += 2 {
				next = append(next, nodeHash(l2[i], l2[i+1]))
			}
			l2 = next
		}
		segLeaves = append(segLeaves, segmentLeaf(s, firstSequence+s*uint64(segmentSize), ac, l2[0]))
	}
	peakPath, targetHeight := mmrPath(segLeaves, int(segIndex))

	return &EventDisclosure{
		EventType:        events[idx].Type,
		EventBody:        events[idx].Canonical,
		Sequence:         sequence,
		SegmentIndex:     segIndex,
		Position:         pos,
		ActualCount:      actualCount,
		SegmentPath:      segPath,
		PeakPath:         peakPath,
		TargetPeakHeight: targetHeight,
	}, nil
}

// merklePath returns the sibling path for position pos and the root.
func merklePath(leaves []Digest, pos uint32) ([]ProofStep, Digest) {
	level := leaves
	p := pos
	var path []ProofStep
	for len(level) > 1 {
		var sibling Digest
		var dir uint8
		if p%2 == 0 {
			sibling = level[p+1]
			dir = 1 // sibling is RIGHT
		} else {
			sibling = level[p-1]
			dir = 0 // sibling is LEFT
		}
		path = append(path, ProofStep{Direction: dir, Digest: sibling})
		next := make([]Digest, 0, len(level)/2)
		for i := 0; i+1 < len(level); i += 2 {
			next = append(next, nodeHash(level[i], level[i+1]))
		}
		level = next
		p /= 2
	}
	return path, level[0]
}

// mmrPath returns the carry path from leaf index to its peak and the
// peak height. It REPLAYS the prefix peaks first (binary carry over
// leaves[0:index]) so left-absorption is faithful to the MMR shape.
func mmrPath(leaves []Digest, index int) ([]ProofStep, uint64) {
	type node struct {
		height uint64
		digest Digest
		index  int
	}
	// Prefix peaks for leaves[0:index].
	var stack []node
	for i := 0; i < index; i++ {
		cur := node{0, leaves[i], i}
		for len(stack) > 0 && stack[len(stack)-1].height == cur.height {
			prev := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			cur = node{prev.height + 1, mmrNode(prev.digest, cur.digest), prev.index}
		}
		stack = append(stack, cur)
	}
	var path []ProofStep
	cur := node{0, leaves[index], index}
	for {
		if len(stack) > 0 && stack[len(stack)-1].height == cur.height {
			prev := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			path = append(path, ProofStep{Direction: 0, Digest: prev.digest}) // sibling LEFT
			cur = node{prev.height + 1, mmrNode(prev.digest, cur.digest), prev.index}
			continue
		}
		rightIdx := cur.index + (1 << cur.height)
		if rightIdx+(1<<cur.height) <= len(leaves) {
			right := foldRange(leaves, rightIdx, int(cur.height))
			path = append(path, ProofStep{Direction: 1, Digest: right}) // sibling RIGHT
			cur = node{cur.height + 1, mmrNode(cur.digest, right), cur.index}
			continue
		}
		return path, cur.height
	}
}

// foldRange folds leaves [start, start+2^height) into one digest.
func foldRange(leaves []Digest, start, height int) Digest {
	if height == 0 {
		return leaves[start]
	}
	mid := start + (1 << (height - 1))
	return mmrNode(foldRange(leaves, start, height-1), foldRange(leaves, mid, height-1))
}
