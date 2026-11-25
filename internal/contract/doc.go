// Package contract is the gate on go-files' promise index, api/contract.txt. It is its tests
// alone; the pre-commit tier runs them. Each line of the index names a test of the package it is
// about, and the gate holds that the test is there, is built on each system the line names and
// can reach no skip there, that the index is sorted with no line twice, and that a line removed
// since the last tag before the head is listed in api/breaks/<base tag>.contract.txt.
package contract
