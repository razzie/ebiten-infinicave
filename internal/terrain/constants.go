package terrain

const (
	BackgroundMinX = -.5
	BackgroundMaxX = 1.5
)

// Width is the fixed cross-axis span of the cave in scene units: its width in
// Vertical scenes and its height in Horizontal scenes.
const Width = 1

// SectionHeight is the edge length of one square streamed section in scene units.
const SectionHeight = 1

const (
	// All generation geometry uses scene units. The owned section is [0,1]²;
	// its generation window adds one unit of padding above and below.
	generationWidth       = Width
	GenerationMinY        = -SectionHeight
	generationMaxY        = 2 * SectionHeight
	GenerationHeight      = generationMaxY - GenerationMinY
	foregroundScreenInset = .018

	guideSpacing   = .020
	guideInfluence = .180
	noiseScale     = 3.4
)
