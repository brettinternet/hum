package output

const terminalEscape = byte(0x1b)

// StripTerminalControl removes terminal control sequences from one output
// entry. It operates on bytes rather than runes so NUL, invalid UTF-8, and
// every byte outside a recognized sequence are preserved exactly. The
// transform is stateless and applies only within text.
//
// CSI sequences are parsed according to ECMA-48's parameter, intermediate, and
// final byte ranges. OSC strings end at BEL or ST; DCS, SOS, PM, and APC strings
// end only at ST. Other ESC sequences consume intermediates followed by one
// final byte. An
// unterminated sequence consumes the rest of the entry. Carriage returns are
// removed only when immediately followed by LF.
func StripTerminalControl(text string) string {
	first := -1
	for i := 0; i < len(text); i++ {
		if text[i] == terminalEscape || (text[i] == '\r' && i+1 < len(text) && text[i+1] == '\n') {
			first = i
			break
		}
	}
	if first < 0 {
		return text
	}

	stripped := make([]byte, 0, len(text))
	stripped = append(stripped, text[:first]...)
	for i := first; i < len(text); {
		switch text[i] {
		case terminalEscape:
			i = terminalEscapeEnd(text, i)
		case '\r':
			if i+1 < len(text) && text[i+1] == '\n' {
				i++
				continue
			}
			stripped = append(stripped, text[i])
			i++
		default:
			stripped = append(stripped, text[i])
			i++
		}
	}
	return string(stripped)
}

// terminalEscapeEnd returns the first byte after the ESC sequence at start.
// Returning len(text) for an incomplete sequence removes the ESC and its
// remaining bytes from this entry.
func terminalEscapeEnd(text string, start int) int {
	if start+1 >= len(text) {
		return len(text)
	}

	introducer := text[start+1]
	if introducer == ']' || introducer == 'P' || introducer == 'X' || introducer == '^' || introducer == '_' {
		for i := start + 2; i < len(text); i++ {
			switch text[i] {
			case '\a':
				if introducer == ']' {
					return i + 1
				}
			case terminalEscape:
				if i+1 < len(text) && text[i+1] == '\\' {
					return i + 2
				}
			}
		}
		return len(text)
	}

	if introducer == '[' {
		i := start + 2
		for i < len(text) && terminalParameterByte(text[i]) {
			i++
		}
		for i < len(text) && terminalIntermediateByte(text[i]) {
			i++
		}
		if i < len(text) && terminalFinalByte(text[i]) {
			return i + 1
		}
		return len(text)
	}

	i := start + 1
	for i < len(text) && terminalIntermediateByte(text[i]) {
		i++
	}
	if i < len(text) && terminalEscapeFinalByte(text[i]) {
		return i + 1
	}
	return len(text)
}

func terminalParameterByte(value byte) bool { return value >= 0x30 && value <= 0x3f }

func terminalIntermediateByte(value byte) bool { return value >= 0x20 && value <= 0x2f }

func terminalFinalByte(value byte) bool { return value >= 0x40 && value <= 0x7e }

func terminalEscapeFinalByte(value byte) bool { return value >= 0x30 && value <= 0x7e }
