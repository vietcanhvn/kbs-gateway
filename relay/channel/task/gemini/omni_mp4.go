package gemini

import (
	"encoding/base64"
	"encoding/binary"
	"strings"
)

// MP4DurationProbe là io.Writer đọc lướt các box cấp cao nhất của tệp MP4 để
// lấy thời lượng trong moov/mvhd, dùng khi tệp đang được truyền qua cổng
// (không cần đệm cả video). Không bao giờ trả lỗi để không làm hỏng luồng tải.
type MP4DurationProbe struct {
	header   [16]byte
	headerN  int
	skip     uint64 // số byte còn lại của box đang bỏ qua (mdat, ftyp, ...)
	moov     []byte
	moovLeft uint64
	inMoov   bool
	done     bool
	seconds  float64
	found    bool
}

// mp4MaxMoovBytes chặn bộ nhớ đệm cho box moov (thường chỉ vài chục KB).
const mp4MaxMoovBytes = 16 << 20

func (p *MP4DurationProbe) Write(b []byte) (int, error) {
	n := len(b)
	for len(b) > 0 && !p.done {
		switch {
		case p.skip > 0:
			step := min(p.skip, uint64(len(b)))
			p.skip -= step
			b = b[step:]
		case p.inMoov:
			step := min(p.moovLeft, uint64(len(b)))
			p.moov = append(p.moov, b[:step]...)
			p.moovLeft -= step
			b = b[step:]
			if p.moovLeft == 0 {
				p.seconds, p.found = mvhdSeconds(p.moov)
				p.done = true
			}
		default:
			b = p.readHeader(b)
		}
	}
	return n, nil
}

// readHeader gom đủ header box (8 byte, hoặc 16 byte khi size==1) rồi quyết định bỏ qua hay đệm.
func (p *MP4DurationProbe) readHeader(b []byte) []byte {
	need := 8
	if p.headerN >= 4 && binary.BigEndian.Uint32(p.header[:4]) == 1 {
		need = 16
	}
	step := min(need-p.headerN, len(b))
	copy(p.header[p.headerN:], b[:step])
	p.headerN += step
	b = b[step:]
	if p.headerN < need {
		return b
	}
	if need == 8 && binary.BigEndian.Uint32(p.header[:4]) == 1 {
		return b // cần thêm 8 byte kích thước 64-bit
	}

	size := uint64(binary.BigEndian.Uint32(p.header[:4]))
	if size == 1 {
		size = binary.BigEndian.Uint64(p.header[8:16])
	}
	boxType := string(p.header[4:8])
	p.headerN = 0
	if size < uint64(need) { // size 0 (tới hết tệp) hoặc hỏng
		p.done = true
		return b
	}
	body := size - uint64(need)
	if boxType != "moov" {
		p.skip = body
		return b
	}
	if body > mp4MaxMoovBytes {
		p.done = true
		return b
	}
	p.inMoov = true
	p.moovLeft = body
	p.moov = make([]byte, 0, body)
	if body == 0 {
		p.done = true
	}
	return b
}

// Seconds trả thời lượng (giây) nếu đã đọc được mvhd.
func (p *MP4DurationProbe) Seconds() (float64, bool) {
	return p.seconds, p.found
}

// mvhdSeconds tìm box mvhd con trực tiếp của moov và tính duration/timescale.
func mvhdSeconds(moov []byte) (float64, bool) {
	for len(moov) >= 8 {
		size := uint64(binary.BigEndian.Uint32(moov[:4]))
		boxType := string(moov[4:8])
		headerLen := uint64(8)
		if size == 1 {
			if len(moov) < 16 {
				return 0, false
			}
			size = binary.BigEndian.Uint64(moov[8:16])
			headerLen = 16
		}
		if size < headerLen || size > uint64(len(moov)) {
			return 0, false
		}
		if boxType == "mvhd" {
			body := moov[headerLen:size]
			if len(body) < 1 {
				return 0, false
			}
			var timescale, duration uint64
			if body[0] == 1 {
				if len(body) < 32 {
					return 0, false
				}
				timescale = uint64(binary.BigEndian.Uint32(body[20:24]))
				duration = binary.BigEndian.Uint64(body[24:32])
			} else {
				if len(body) < 20 {
					return 0, false
				}
				timescale = uint64(binary.BigEndian.Uint32(body[12:16]))
				duration = uint64(binary.BigEndian.Uint32(body[16:20]))
			}
			if timescale == 0 {
				return 0, false
			}
			return float64(duration) / float64(timescale), true
		}
		moov = moov[size:]
	}
	return 0, false
}

// MP4DurationSeconds đọc thời lượng của một tệp MP4 nằm trọn trong bộ nhớ.
func MP4DurationSeconds(data []byte) (float64, bool) {
	var probe MP4DurationProbe
	_, _ = probe.Write(data)
	return probe.Seconds()
}

// decodeBase64Loose giải base64 chuẩn, có hoặc không có dấu "=".
func decodeBase64Loose(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if decoded, err := base64.StdEncoding.DecodeString(s); err == nil {
		return decoded, nil
	}
	return base64.RawStdEncoding.DecodeString(strings.TrimRight(s, "="))
}
