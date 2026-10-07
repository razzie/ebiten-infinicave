package terrain

func VineFamily(vines []Vine, index int) int {
	family := index
	for vines[family].Parent >= 0 && vines[family].Parent < family {
		family = vines[family].Parent
	}
	return family
}

func VineStyleFamily(vines []Vine, index int) int {
	if vines[index].styleSet {
		return vines[index].styleFamily
	}
	return VineFamily(vines, index)
}
