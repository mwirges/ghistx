// Package ngram generates 3-character n-grams from a byte string.
//
// The algorithm bit-packs three consecutive bytes into a uint32:
// each byte is shifted left by 8 bits and OR'd with the next byte,
// masked to 24 bits. This is byte-level (not rune-level) to stay
// compatible with the C histx implementation.
package ngram

// Gen returns all 3-gram values for s.
// For compatibility with the C implementation, it operates on bytes.
// A string with fewer than 3 bytes produces no n-grams.
func Gen(s string) []uint32 {
	const mask = uint32(0xffffff)
	b := []byte(s)
	if len(b) < 3 {
		return nil
	}
	result := make([]uint32, 0, len(b)-2)
	var ng uint32
	for i, c := range b {
		ng = ((ng << 8) & mask) | uint32(c)
		if i >= 2 {
			result = append(result, ng)
		}
	}
	return result
}
