package generator

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"path/filepath"

	"github.com/gosimple/slug"
	"github.com/lectio/content"
	"github.com/lectio/score"
	"gopkg.in/cheggaaa/pb.v1"
)

const facebookGraphFileExtn = ".facebook.json"
const facebookGraphErrorFileExtn = ".facebook-error.json"
const linkedInCountFileExtn = ".linkedin.json"
const linkedInCountErrorFileExtn = ".linkedin-error.json"

// HugoGenerator is the primary Hugo content generator engine
type HugoGenerator struct {
	collection                         content.Collection
	homePath                           string
	contentID                          string
	contentPath                        string
	scoresDataPath                     string
	simulateSocialScores               bool
	verbose                            bool
	createDestPaths                    bool
	itemsInCollectionCount             int
	itemsGeneratedCount                int
	itemsWithFacebookGraphInvalidCount int
	itemsWithLinkedInGraphInvalidCount int
	errors                             []error
}

// HugoContentTime is a convenience type for storing content timestamp
type HugoContentTime time.Time

// MarshalJSON stores HugoContentTime in a manner readable by Hugo
func (hct HugoContentTime) MarshalJSON() ([]byte, error) {
	stamp := fmt.Sprintf("\"%s\"", time.Time(hct).Format("Mon Jan 2 15:04:05 MST 2006"))
	return []byte(stamp), nil
}

// HugoContent is a single Hugo page/content
type HugoContent struct {
	Link              string                              `json:"link"`
	Title             string                              `json:"title"`
	Summary           string                              `json:"description"`
	Body              string                              `json:"content"`
	Categories        []string                            `json:"categories"`
	CreatedOn         HugoContentTime                     `json:"date"`
	FeaturedImage     string                              `json:"featuredimage"`
	Source            string                              `json:"source"`
	Slug              string                              `json:"slug"`
	GloballyUniqueKey string                              `json:"uniquekey"`
	TotalSharesCount  int                                 `json:"totalSharesCount"`
	FacebookLinkScore *score.FacebookLinkScoreGraphResult `json:"fbgraph"`
	LinkedInLinkScore *score.LinkedInLinkScoreResult      `json:"ligraph"`
}

// NewHugoGenerator creates the default Hugo generation engine
func NewHugoGenerator(collection content.Collection, homePath string, contentID string, createDestPaths bool, verbose bool, simulateSocialScores bool) (*HugoGenerator, error) {
	result := new(HugoGenerator)
	result.collection = collection
	result.homePath = homePath
	result.contentID = contentID
	result.contentPath = filepath.Join(homePath, "content", contentID)
	result.scoresDataPath = filepath.Join(homePath, "data", "scores", contentID)
	result.simulateSocialScores = simulateSocialScores
	result.verbose = verbose
	result.createDestPaths = createDestPaths

	if createDestPaths {
		if _, err := createDirIfNotExist(result.contentPath); err != nil {
			return result, fmt.Errorf("Unable to create content path %q: %v", result.contentPath, err)
		}
		if _, err := createDirIfNotExist(result.scoresDataPath); err != nil {
			return result, fmt.Errorf("Unable to create scores data path %q: %v", result.scoresDataPath, err)
		}
	}

	if _, err := os.Stat(result.contentPath); os.IsNotExist(err) {
		return result, fmt.Errorf("content path %q does not exist: %v", result.contentPath, err)
	}
	if _, err := os.Stat(result.scoresDataPath); os.IsNotExist(err) {
		return result, fmt.Errorf("scores data path %q does not exist: %v", result.scoresDataPath, err)
	}

	return result, nil
}

// Errors records any issues with the generator as its processing its entries
func (g HugoGenerator) Errors() []error {
	return g.errors
}

// GetContentFilename returns the name of the file a given piece of HugoContent
func (g HugoGenerator) GetContentFilename(gc *HugoContent) string {
	return fmt.Sprintf("%s.md", filepath.Join(g.contentPath, gc.Slug))
}

// GetActivitySummary returns a useful message about the activities that the engine performed
func (g HugoGenerator) GetActivitySummary() string {
	return fmt.Sprintf("Generated %d posts in %q from %d items read (%q), Simulating scores: %v, Facebook Graph errors: %d, LinkedIn Graph errors: %d",
		g.itemsGeneratedCount, g.contentPath, g.itemsInCollectionCount, g.collection.Source(),
		g.simulateSocialScores, g.itemsWithFacebookGraphInvalidCount, g.itemsWithLinkedInGraphInvalidCount)
}

// GenerateContent executes the engine (creates all the Hugo files from the given collection)
func (g *HugoGenerator) GenerateContent() error {
	items := g.collection.Content()
	g.itemsInCollectionCount = len(items)
	var bar *pb.ProgressBar
	if g.verbose {
		bar = pb.StartNew(g.itemsInCollectionCount)
		bar.ShowCounters = true
	}
	for i := 0; i < len(items); i++ {
		source := items[i]
		var genContent HugoContent
		var scores score.LinkScores

		ogTitle, ok := source.Title().OpenGraphTitle()
		if ok {
			genContent.Title = ogTitle
		} else {
			genContent.Title = source.Title().Clean()
		}

		ogDescr, ok := source.Summary().OpenGraphDescription()
		if ok {
			genContent.Summary = ogDescr
		} else {
			firstSentence, fsErr := source.Summary().FirstSentenceOfBody()
			if fsErr == nil {
				genContent.Summary = firstSentence
			} else {
				genContent.Summary = source.Summary().Original()
			}
		}
		genContent.Body = source.Body()
		genContent.Categories = source.Categories()
		genContent.CreatedOn = HugoContentTime(source.CreatedOn())
		if source.FeaturedImage() != nil {
			genContent.FeaturedImage = source.FeaturedImage().String()
		}

		switch v := source.(type) {
		case content.CuratedContent:
			resource := v.TargetResource()
			if resource == nil {
				g.errors = append(g.errors, fmt.Errorf("skipping item %d in HugoGenerator, it has nil TargetResource()", i))
				continue
			}
			isURLValid, isDestValid := resource.IsValid()
			if !isURLValid || !isDestValid {
				g.errors = append(g.errors, fmt.Errorf("skipping item %d due to invalid resource URL %q; isURLValid: %v, isDestValid: %v", i, resource.OriginalURLText(), isURLValid, isDestValid))
				continue
			}
			finalURL, _, _ := resource.GetURLs()
			if finalURL == nil || len(finalURL.String()) == 0 {
				g.errors = append(g.errors, fmt.Errorf("skipping item %d in HugoGenerator, finalURL is nil or empty string", i))
				continue
			}
			scores = score.GetLinkScores(finalURL, resource.GloballyUniqueKey(), score.DefaultInitialTotalSharesCount, g.simulateSocialScores)
			genContent.Link = finalURL.String()
			genContent.Source = content.GetSimplifiedHostname(finalURL)
			genContent.Slug = slug.Make(content.GetSimplifiedHostnameWithoutTLD(finalURL) + "-" + source.Title().Clean())
			genContent.GloballyUniqueKey = resource.GloballyUniqueKey()
			genContent.TotalSharesCount = scores.TotalSharesCount()
			genContent.FacebookLinkScore = scores.FacebookLinkScore()
			genContent.LinkedInLinkScore = scores.LinkedInLinkScore()
			if !genContent.FacebookLinkScore.IsValid() {
				g.itemsWithFacebookGraphInvalidCount++
			}
			if !genContent.LinkedInLinkScore.IsValid() {
				g.itemsWithLinkedInGraphInvalidCount++
			}

		case content.Content:
			genContent.Slug = slug.Make(source.Title().Clean())
		default:
			fmt.Printf("I don't know about type %T!\n", v)
		}

		_, err := genContent.createFile(g)
		if err != nil {
			g.errors = append(g.errors, fmt.Errorf("error writing item %d in HugoGenerator: %+v", i, err))
		}
		g.itemsGeneratedCount++
		if g.verbose {
			bar.Increment()
		}
	}
	if g.verbose {
		bar.FinishPrint(fmt.Sprintf("Completed generating %d Hugo items from %q", g.itemsGeneratedCount, g.collection.Source()))
	}

	return nil
}

func (c *HugoContent) createFile(g *HugoGenerator) (string, error) {
	fileName := g.GetContentFilename(c)
	file, createErr := os.Create(fileName)
	if createErr != nil {
		return fileName, fmt.Errorf("Unable to create file %q: %v", fileName, createErr)
	}
	defer file.Close()

	frontMatter, fmErr := json.MarshalIndent(c, "", "	")
	if fmErr != nil {
		return fileName, fmt.Errorf("Unable to marshal front matter %q: %v", fileName, fmErr)
	}

	_, writeErr := file.Write(frontMatter)
	if writeErr != nil {
		return fileName, fmt.Errorf("Unable to write front matter %q: %v", fileName, writeErr)
	}

	_, writeErr = file.WriteString("\n\n" + c.Body)
	if writeErr != nil {
		return fileName, fmt.Errorf("Unable to write content body %q: %v", fileName, writeErr)
	}

	return fileName, nil
}

// createDirIfNotExist creates a path if it does not exist. It is similar to mkdir -p in shell command,
// which also creates parent directory if not exists.
func createDirIfNotExist(dir string) (bool, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err = os.MkdirAll(dir, 0755)
		return true, err
	}
	return false, nil
}
