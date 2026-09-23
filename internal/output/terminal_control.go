package output

const (
	terminalEscape = byte(0x1b)
	// terminalC1Lead is the first UTF-8 byte of U+0080-U+009F, the C1 control
	// set, which terminals such as xterm and VTE interpret like ESC sequences.
	terminalC1Lead = byte(0xc2)
)

// StripTerminalControl removes terminal control sequences from one output
// entry. It operates on bytes rather than runes so NUL, invalid UTF-8, and
// every byte outside a recognized sequence are preserved exactly. The
// transform is stateless and applies only within text.
//
// A sequence starts at ESC or at a UTF-8 encoded C1 control, which is
// equivalent to ESC followed by the control's code minus 0x40; an unencoded
// 8-bit C1 byte is invalid UTF-8 and is preserved. CSI sequences are parsed
// according to ECMA-48's parameter, intermediate, and final byte ranges. OSC
// strings end at BEL or ST; DCS, SOS, PM, and APC strings end only at ST, in
// either form. Other ESC sequences consume intermediates followed by one final
// byte, and other C1 controls are removed alone. An unterminated sequence
// consumes the rest of the entry. Carriage returns are removed only when
// immediately followed by LF.
func StripTerminalControl(text string) string {
	first := -1
	for i := 0; i < len(text); i++ {
		if text[i] == terminalEscape || terminalC1Control(text, i) || (text[i] == '\r' && i+1 < len(text) && text[i+1] == '\n') {
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
		switch {
		case text[i] == terminalEscape:
			i = terminalEscapeEnd(text, i)
		case terminalC1Control(text, i):
			if end, ok := terminalSequenceEnd(text, text[i+1]-0x40, i+2); ok {
				i = end
			} else {
				i += 2
			}
		case text[i] == '\r' && i+1 < len(text) && text[i+1] == '\n':
			i++
		default:
			stripped = append(stripped, text[i])
			i++
		}
	}
	return string(stripped)
}

func terminalC1Control(text string, i int) bool {
	return text[i] == terminalC1Lead && i+1 < len(text) && text[i+1] >= 0x80 && text[i+1] <= 0x9f
}

// terminalEscapeEnd returns the first byte after the ESC sequence at start.
// Returning len(text) for an incomplete sequence removes the ESC and its
// remaining bytes from this entry.
func terminalEscapeEnd(text string, start int) int {
	if start+1 >= len(text) {
		return len(text)
	}
	if end, ok := terminalSequenceEnd(text, text[start+1], start+2); ok {
		return end
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

// terminalSequenceEnd returns the first byte after a CSI sequence or control
// string whose 7-bit introducer is introducer and whose body starts at body.
// It reports false for any other introducer.
func terminalSequenceEnd(text string, introducer byte, body int) (int, bool) {
	switch introducer {
	case ']', 'P', 'X', '^', '_':
		for i := body; i < len(text); i++ {
			switch {
			case text[i] == '\a' && introducer == ']':
				return i + 1, true
			case text[i] == terminalEscape && i+1 < len(text) && text[i+1] == '\\':
				return i + 2, true
			case text[i] == terminalC1Lead && i+1 < len(text) && text[i+1] == 0x9c:
				return i + 2, true
			}
		}
		return len(text), true
	case '[':
		i := body
		for i < len(text) && terminalParameterByte(text[i]) {
			i++
		}
		for i < len(text) && terminalIntermediateByte(text[i]) {
			i++
		}
		if i < len(text) && terminalFinalByte(text[i]) {
			return i + 1, true
		}
		return len(text), true
	}
	return 0, false
}

func terminalParameterByte(value byte) bool { return value >= 0x30 && value <= 0x3f }

func terminalIntermediateByte(value byte) bool { return value >= 0x20 && value <= 0x2f }

func terminalFinalByte(value byte) bool { return value >= 0x40 && value <= 0x7e }

func terminalEscapeFinalByte(value byte) bool { return value >= 0x30 && value <= 0x7e }
