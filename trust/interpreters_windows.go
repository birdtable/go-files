package trust

// kernel reads no #! line: Windows starts no interpreter a file names.
func kernel() shebang { return shebang{} }

// openNoWait is nothing: Windows reads no name here.
const openNoWait = 0
