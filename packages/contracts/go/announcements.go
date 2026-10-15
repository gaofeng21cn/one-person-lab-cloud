package contracts

// CloudSessionHeader carries a CloudIdentity session reference on the retained
// Control Plane's typed HTTP boundary. The owner revalidates it; it is never an
// actor, Tenant or retained-session assertion.
const CloudSessionHeader = "x-opl-cloud-session"

// Announcement remains a Control Plane-owned publication. Cloud and retained
// Console readers consume the same publication and acknowledgement records.
type Announcement struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	Status      string `json:"status"`
	StartsAt    string `json:"startsAt,omitempty"`
	EndsAt      string `json:"endsAt,omitempty"`
	PublishedAt string `json:"publishedAt,omitempty"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
	Read        bool   `json:"read"`
}

type AnnouncementPage struct {
	Items    []Announcement `json:"items"`
	Total    int            `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"pageSize"`
}

type AnnouncementRead struct {
	AnnouncementID string `json:"announcementId"`
	ReadAt         string `json:"readAt"`
}

// AnnouncementSource distinguishes an actual empty page from an unavailable
// owner, using the retained Console's source envelope.
type AnnouncementSource struct {
	Source          string            `json:"source"`
	Status          string            `json:"status"`
	Available       bool              `json:"available"`
	Data            *AnnouncementPage `json:"data,omitempty"`
	FetchedAt       string            `json:"fetchedAt"`
	SourceUpdatedAt string            `json:"sourceUpdatedAt,omitempty"`
	ReasonCode      string            `json:"reasonCode,omitempty"`
}
