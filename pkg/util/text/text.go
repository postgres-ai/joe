package text

// Cuts length of a text if it exceeds specified size. Specifies was text cut or not.
func CutText(text string, size int, separator string) (string, bool) {
	if len(text) > size {
		size -= len(separator)
		res := text[0:size] + separator
		return res, true
	}

	return text, false
}
