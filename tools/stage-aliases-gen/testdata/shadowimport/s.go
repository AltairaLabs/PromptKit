package core

import "example.com/x/corepkg"

// ClashImport hides the package, and names an import called like its second
// import.
func ClashImport(core int, v corepkg.V) int { return core }
