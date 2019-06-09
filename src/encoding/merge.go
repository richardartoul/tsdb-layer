package encoding

// MergeStreams merges a list of streams into a a single stream.
func MergeStreams(streams ...[]byte) ([]byte, error) {
	decoders := make([]Decoder, 0, len(streams))
	for _, stream := range streams {
		dec := NewDecoder()
		dec.Reset(stream)
		decoders = append(decoders, dec)
	}

	multiDec := NewMultiDecoder()
	multiDec.Reset(decoders)

	mergedEnc := NewEncoder()
	for multiDec.Next() {
		mergedEnc.Encode(multiDec.Current())
	}
	if err := multiDec.Err(); err != nil {
		return nil, err
	}

	return mergedEnc.Bytes(), nil
}
