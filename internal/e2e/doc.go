// Package e2e drives examples/mytool as a subprocess against a local
// GitHub-shaped release server. TestMain builds the demo twice with
// different versions; the scenarios update one build to the other and
// inspect the binary on disk. Run go test -short to skip the suite.
package e2e
