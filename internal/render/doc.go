// Package render turns terrain geometry into meshes and manages shaders and
// effects. CPU mesh preparation can run on workers; GPU work stays on the game
// goroutine. This package does not own scene availability or query identity.
package render
