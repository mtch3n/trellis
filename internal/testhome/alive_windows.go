package testhome

// processAlive cannot answer on Windows: OpenProcess needs privileges a test
// run may not have, and a recycled pid would be indistinguishable anyway. A
// leftover home there is collected on age alone (see staleAfter).
func processAlive(int) bool { return true }
