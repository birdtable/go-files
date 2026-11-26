package gofiles_test

// handedLoadLine is what hatch budget says of a tier that ends within its budget with a load read at
// its start. Windows has no load average, so hatch cannot read the load at the end.
// hatch prints the load before only beside one at the end, so here it says the load is unknown,
// and from v0.7.0 it gives go-proc's reason after a colon, or nothing. The load before still
// reaches hatch here: the rows of TestBudgetFailsALateTierButACommitStartedAboveLoad15 that
// start above 15 pass only with it.
const handedLoadLine = `^budget: load unknown(: |$)`
