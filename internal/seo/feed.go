package seo

import (
	"encoding/xml"
	"strings"
	"time"
)

// rss 是 RSS 2.0 文档。
type rss struct {
	XMLName xml.Name `xml:"rss"`
	Version string   `xml:"version,attr"`
	// XMLNSDC 声明 dc 前缀，item 的 dc:creator 用到它；缺了整篇 RSS 就不是合法 XML。
	XMLNSDC string     `xml:"xmlns:dc,attr"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	Description string    `xml:"description"`
	Language    string    `xml:"language,omitempty"`
	LastBuild   string    `xml:"lastBuildDate,omitempty"`
	Generator   string    `xml:"generator"`
	Items       []rssItem `xml:"item"`
}

type rssItem struct {
	Title   string   `xml:"title"`
	Link    string   `xml:"link"`
	GUID    rssGUID  `xml:"guid"`
	PubDate string   `xml:"pubDate,omitempty"`
	Desc    string   `xml:"description"`
	Creator string   `xml:"dc:creator,omitempty"`
	Cats    []string `xml:"category,omitempty"`
}

type rssGUID struct {
	Value       string `xml:",chardata"`
	IsPermaLink bool   `xml:"isPermaLink,attr"`
}

// atom 是 Atom 1.0 文档。
type atom struct {
	XMLName xml.Name    `xml:"feed"`
	XMLNS   string      `xml:"xmlns,attr"`
	Title   string      `xml:"title"`
	Subttl  string      `xml:"subtitle,omitempty"`
	ID      string      `xml:"id"`
	Link    []atomLink  `xml:"link"`
	Updated string      `xml:"updated"`
	Author  atomAuthor  `xml:"author"`
	Entries []atomEntry `xml:"entry"`
}

type atomLink struct {
	Rel  string `xml:"rel,attr,omitempty"`
	Href string `xml:"href,attr"`
	Type string `xml:"type,attr,omitempty"`
}

type atomAuthor struct {
	Name string `xml:"name"`
}

type atomEntry struct {
	Title   string     `xml:"title"`
	Link    atomLink   `xml:"link"`
	ID      string     `xml:"id"`
	Updated string     `xml:"updated"`
	Summary string     `xml:"summary"`
	Content string     `xml:"content,omitempty"`
	Author  atomAuthor `xml:"author"`
	Cats    []string   `xml:"category,omitempty"`
}

// rfc822 与 rfc3339 是两种订阅格式各自的时间表示。
const (
	rfc822  = "Mon, 02 Jan 2006 15:04:05 -0700"
	rfc3339 = time.RFC3339
)

// nsDublinCore 是 dc:creator 所属的 Dublin Core 命名空间。
const nsDublinCore = "http://purl.org/dc/elements/1.1/"

// feedMeta 是生成订阅源所需的站点信息。
type feedMeta struct {
	Title       string
	Description string
	Language    string
	SiteURL     string
	SelfRSS     string
	SelfAtom    string
}

// renderRSS 生成 RSS 2.0 文档。
func renderRSS(meta *feedMeta, entries []Entry) ([]byte, error) {
	ch := rssChannel{
		Title:       meta.Title,
		Link:        meta.SiteURL,
		Description: meta.Description,
		Language:    meta.Language,
		Generator:   "Lumo",
		Items:       make([]rssItem, 0, len(entries)),
	}
	if len(entries) > 0 {
		ch.LastBuild = entries[0].UpdatedAt.Format(rfc822)
	}

	for i := range entries {
		e := &entries[i]
		item := rssItem{
			Title:   e.Title,
			Link:    e.URL,
			GUID:    rssGUID{Value: e.URL, IsPermaLink: true},
			Desc:    e.Summary,
			Creator: e.AuthorName,
		}
		if !e.PublishedAt.IsZero() {
			item.PubDate = e.PublishedAt.Format(rfc822)
		}
		ch.Items = append(ch.Items, item)
	}
	return marshal(rss{Version: "2.0", XMLNSDC: nsDublinCore, Channel: ch})
}

// renderAtom 生成 Atom 1.0 文档。
func renderAtom(meta *feedMeta, entries []Entry) ([]byte, error) {
	doc := atom{
		XMLNS:   "http://www.w3.org/2005/Atom",
		Title:   meta.Title,
		Subttl:  meta.Description,
		ID:      meta.SiteURL,
		Updated: time.Now().Format(rfc3339),
		Author:  atomAuthor{Name: meta.Title},
		Link: []atomLink{
			{Rel: "alternate", Href: meta.SiteURL, Type: "text/html"},
			{Rel: "self", Href: meta.SelfAtom, Type: "application/atom+xml"},
		},
		Entries: make([]atomEntry, 0, len(entries)),
	}
	if len(entries) > 0 {
		doc.Updated = entries[0].UpdatedAt.Format(rfc3339)
	}

	for i := range entries {
		e := &entries[i]
		entry := atomEntry{
			Title:   e.Title,
			Link:    atomLink{Rel: "alternate", Href: e.URL, Type: "text/html"},
			ID:      e.URL,
			Updated: e.UpdatedAt.Format(rfc3339),
			Summary: e.Summary,
			Author:  atomAuthor{Name: e.AuthorName},
		}
		if e.ContentHTML != "" {
			entry.Content = e.ContentHTML
		}
		doc.Entries = append(doc.Entries, entry)
	}
	return marshal(doc)
}

// marshal 输出带 XML 声明与缩进的文档。
func marshal(v any) ([]byte, error) {
	body, err := xml.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), body...), nil
}

// sitemap 是 sitemap.xml 文档。
type sitemap struct {
	XMLName xml.Name       `xml:"urlset"`
	XMLNS   string         `xml:"xmlns,attr"`
	URLs    []sitemapEntry `xml:"url"`
}

type sitemapEntry struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

// sitemapDate 是 sitemap 协议要求的日期精度。
const sitemapDate = "2006-01-02"

// renderSitemap 生成 sitemap.xml 文档。
func renderSitemap(urls []Entry) ([]byte, error) {
	doc := sitemap{
		XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs:  make([]sitemapEntry, 0, len(urls)),
	}
	for i := range urls {
		u := &urls[i]
		entry := sitemapEntry{Loc: u.URL}
		if !u.UpdatedAt.IsZero() {
			entry.LastMod = u.UpdatedAt.Format(sitemapDate)
		}
		doc.URLs = append(doc.URLs, entry)
	}
	return marshal(doc)
}

// renderRobots 生成 robots.txt。
//
// 默认规则只挡后台与 API：前台内容本就该被索引，而 Console 与接口路径被收录
// 只会给爬虫制造一堆 401。
func renderRobots(siteURL, extra string, sitemapEnabled bool) []byte {
	var b strings.Builder
	b.WriteString("User-agent: *\n")
	for _, path := range []string{"/console/", "/api/"} {
		b.WriteString("Disallow: " + path + "\n")
	}
	if extra = strings.TrimSpace(extra); extra != "" {
		b.WriteString("\n")
		b.WriteString(extra)
		b.WriteString("\n")
	}
	if sitemapEnabled && siteURL != "" {
		b.WriteString("\nSitemap: " + strings.TrimSuffix(siteURL, "/") + "/sitemap.xml\n")
	}
	return []byte(b.String())
}
