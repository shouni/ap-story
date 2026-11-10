// Package domain provides ap-story's framework-independent domain logic.
package domain

import (
	"fmt"
	"path"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-utils/jobid"
)

// jobIDPrefix is the prefix of job IDs minted by ap-story.
const jobIDPrefix = "c"

// comicsPrefix is the root GCS prefix that holds the artifacts of comic jobs.
const comicsPrefix = "comics"

// designJobsPrefix is the root GCS prefix that holds the state of standalone design-sheet
// jobs. Keeping it apart from comics/ lets the history list (a listing of comics/) list
// "comic jobs only" without reading state, which avoids loading everything before paging.
const designJobsPrefix = "design-jobs"

// NewJobID mints a new job ID.
// The format is "c-<UTC date>-<UTC time>-<12 random hex digits>" (e.g. "c-20260717-113045-1a2b3c4d5e6f").
//
// The minting rule lives in go-utils/jobid, as does the validation rule. Having generation and the
// recovery of the embedded time (jobid.CreatedAt / jobid.SortKey) in the same package prevents
// the accident of one side's format changing and becoming unreadable.
//
// Up to v1.5.0 IDs were minted as "c<UTC date>-<UTC time>-<8 random hex digits>".
// IDs minted then stay in GCS artifact paths, but jobid.CreatedAt reads both
// formats, so the history's dates and ordering hold even when they are mixed.
// Lexical comparison separates old from new, though, so always sort with
// jobid.SortKey ("c-" is smaller than "c2").
func NewJobID() (string, error) {
	return jobid.New(jobIDPrefix)
}

// ValidateJobID checks that a job ID is safe to use in routing and storage paths.
// HTTP input and GCS path construction must always go through this check (or SanitizeJobID).
//
// The validation rule lives in go-utils/jobid. Job IDs are exchanged with the sister projects
// through the MCP server, so if services disagreed on "what counts as a valid ID",
// one could reject an ID the other had issued.
func ValidateJobID(jobID string) error {
	return jobid.Validate(jobID)
}

// SanitizeJobID normalises a path-like value into a safe job ID.
// Path traversal ("../" and the like) is removed by taking the last element, and the rest is validated.
func SanitizeJobID(jobID string) (string, error) {
	return jobid.Sanitize(jobID)
}

// JobObjectPrefix returns the GCS prefix (a relative path without the bucket name) that holds
// the artifacts of a validated job ID. E.g. "comics/c20260717-113045-1a2b3c4d"
func JobObjectPrefix(jobID string) (string, error) {
	safeJobID, err := SanitizeJobID(jobID)
	if err != nil {
		return "", err
	}
	return path.Join(comicsPrefix, safeJobID), nil
}

// JobOutputDir returns the full GCS URI that holds the artifacts of a validated job ID
// (e.g. "gs://bucket/comics/c20260717-113045-1a2b3c4d").
// It can be passed as is to go-comic-kit's operations (GenerateOptions.OutputDir and so on).
func JobOutputDir(bucket, jobID string) (string, error) {
	prefix, err := JobObjectPrefix(jobID)
	if err != nil {
		return "", err
	}
	return remoteio.BuildURI(remoteio.SchemeGCS, bucket, prefix), nil
}

// DesignJobOutputDir returns the full GCS URI that holds the state of a standalone design-sheet
// job (e.g. "gs://bucket/design-jobs/c20260717-113045-1a2b3c4d").
// The generated images themselves are saved under CharacterAssetDir (character/{tag}/), so
// only comic_state.json is placed here.
func DesignJobOutputDir(bucket, jobID string) (string, error) {
	safeJobID, err := SanitizeJobID(jobID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("gs://%s/%s", bucket, path.Join(designJobsPrefix, safeJobID)), nil
}

// CharacterAssetDir returns the GCS bucket root URI (e.g. "gs://bucket") where shared character
// assets (design sheets and so on) that belong to no particular job (work) are saved.
// Characters are shared assets that several works can refer to, so they live outside comics/{jobID}/.
// Passed to go-comic-kit's DesignSheetRequest.OutputDir, it resolves to
// "character/{tag}/{JobID}.ext".
func CharacterAssetDir(bucket string) string {
	return remoteio.BuildURI(remoteio.SchemeGCS, bucket, "")
}
