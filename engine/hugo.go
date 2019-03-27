package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"path/filepath"

	"github.com/gosimple/slug"
	"github.com/lectio/content"
	"github.com/lectio/harvester"
	"github.com/lectio/score"
)

// HugoGenerator is the primary Hugo content generator engine
type HugoGenerator struct {
	collection                         content.Collection
	destinationPath                    string
	simulateSocialScores               bool
	printFileNameAsGenerated           bool
	itemsInCollectionCount             int
	itemsGeneratedCount                int
	itemsWithFacebookGraphInvalidCount int
	itemsWithLinkedInGraphInvalidCount int
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
func NewHugoGenerator(collection content.Collection, destinationPath string, printFileNameAsGenerated bool, simulateSocialScores bool) *HugoGenerator {
	result := new(HugoGenerator)
	result.collection = collection
	result.destinationPath = destinationPath
	result.simulateSocialScores = simulateSocialScores
	result.printFileNameAsGenerated = printFileNameAsGenerated
	return result
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
	for i := 0; i < len(items); i++ {
		source := items[i]
		var genContent HugoContent
		var scores score.LinkScores

		genContent.Title = source.Title().Clean()
		genContent.Summary = source.Summary()
		genContent.Body = source.Body()
		genContent.Categories = source.Categories()
		genContent.CreatedOn = HugoContentTime(source.CreatedOn())
		if source.FeaturedImage() != nil {
			genContent.FeaturedImage = source.FeaturedImage().String()
		}

		switch v := source.(type) {
		case content.CuratedLink:
			url := v.Target()
			scores = score.GetLinkScores(url, score.DefaultInitialTotalSharesCount, g.simulateSocialScores)
			genContent.Link = url.String()
			genContent.Source = harvester.GetSimplifiedHostname(url)
			genContent.Slug = slug.Make(harvester.GetSimplifiedHostnameWithoutTLD(url) + "-" + source.Title().Clean())
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

		fileName, err := genContent.createFile(g)
		if err != nil {
			panic(err)
		}
		g.itemsGeneratedCount++
		if g.printFileNameAsGenerated {
			fmt.Printf("File %d of %d: %s\n", g.itemsGeneratedCount, g.itemsInCollectionCount, fileName)
		}
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
