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

// HugoGenerator is the primary Hugo content generator engine
type HugoGenerator struct {
	collection                         content.Collection
	destinationPath                    string
	simulateSocialScores               bool
	verbose                            bool
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
	Link             string                         `json:"link"`
	Title            string                         `json:"title"`
	Summary          string                         `json:"description"`
	Body             string                         `json:"content"`
	Categories       []string                       `json:"categories"`
	CreatedOn        HugoContentTime                `json:"date"`
	FeaturedImage    string                         `json:"featuredimage"`
	Source           string                         `json:"source"`
	Slug             string                         `json:"slug"`
	TotalSharesCount int                            `json:"totalSharesCount"`
	FacebookGraph    *score.FacebookGraphResult     `json:"fbgraph"`
	LinkedInGraph    *score.LinkedInCountServResult `json:"ligraph"`
}

// NewHugoGenerator creates the default Hugo generation engine
func NewHugoGenerator(collection content.Collection, destinationPath string, verbose bool, simulateSocialScores bool) *HugoGenerator {
	result := new(HugoGenerator)
	result.collection = collection
	result.destinationPath = destinationPath
	result.simulateSocialScores = simulateSocialScores
	result.verbose = verbose
	return result
}

// Errors records any issues with the generator as its processing its entries
func (g HugoGenerator) Errors() []error {
	return g.errors
}

// GetContentFilename returns the name of the file a given piece of HugoContent
func (g HugoGenerator) GetContentFilename(gc *HugoContent) string {
	return fmt.Sprintf("%s.md", filepath.Join(g.destinationPath, gc.Slug))
}

// GetActivitySummary returns a useful message about the activities that the engine performed
func (g HugoGenerator) GetActivitySummary() string {
	return fmt.Sprintf("Generated %d posts in %q from %d items read (%q), Simulating scores: %v, Facebook Graph errors: %d, LinkedIn Graph errors: %d",
		g.itemsGeneratedCount, g.destinationPath, g.itemsInCollectionCount, g.collection.Source(),
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
			scores = score.GetLinkScores(finalURL, score.DefaultInitialTotalSharesCount, g.simulateSocialScores)
			genContent.Link = finalURL.String()
			genContent.Source = content.GetSimplifiedHostname(finalURL)
			genContent.Slug = slug.Make(content.GetSimplifiedHostnameWithoutTLD(finalURL) + "-" + source.Title().Clean())
			genContent.TotalSharesCount = scores.TotalSharesCount()
			genContent.FacebookGraph = scores.FacebookGraph()
			genContent.LinkedInGraph = scores.LinkedInCount()
			if !genContent.FacebookGraph.IsValid() {
				g.itemsWithFacebookGraphInvalidCount++
			}
			if !genContent.LinkedInGraph.IsValid() {
				g.itemsWithLinkedInGraphInvalidCount++
			}

		case content.Content:
			genContent.Slug = source.Keys().Slug()
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
