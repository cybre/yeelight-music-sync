package utils

func IntToRGB(rgb uint) (uint8, uint8, uint8) {
	r := (rgb >> 16) & 0xFF
	g := (rgb >> 8) & 0xFF
	b := rgb & 0xFF

	return uint8(r), uint8(g), uint8(b)
}

func RGBToInt(r, g, b uint8) uint {
	return uint(r)*65536 + uint(g)*256 + uint(b)
}
