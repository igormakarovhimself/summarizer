package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type Meeting struct {
	ID            int
	UserID        int64
	Title         string
	Transcription string
	Summary       string
	CreatedAt     time.Time
}

type MeetingRepo struct {
	db *sql.DB
}

func NewMeetingRepo(db *sql.DB) *MeetingRepo {
	return &MeetingRepo{db: db}
}

func (r *MeetingRepo) Create(ctx context.Context, userID int64, title, transcription, summary string) (int, error) {
	var id int
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO meetings (user_id, title, transcription, summary)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id`,
		userID, title, transcription, summary,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create meeting: %w", err)
	}
	return id, nil
}

func (r *MeetingRepo) GetByID(ctx context.Context, meetingID int) (*Meeting, error) {
	m := &Meeting{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, user_id, title, transcription, summary, created_at
		 FROM meetings WHERE id = $1`,
		meetingID,
	).Scan(&m.ID, &m.UserID, &m.Title, &m.Transcription, &m.Summary, &m.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get meeting by id: %w", err)
	}
	return m, nil
}

func (r *MeetingRepo) ListByUser(ctx context.Context, userID int64) ([]Meeting, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, title, transcription, summary, created_at
		 FROM meetings WHERE user_id = $1
		 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list meetings: %w", err)
	}
	defer rows.Close()

	var meetings []Meeting
	for rows.Next() {
		var m Meeting
		if err := rows.Scan(&m.ID, &m.UserID, &m.Title, &m.Transcription, &m.Summary, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan meeting: %w", err)
		}
		meetings = append(meetings, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	return meetings, nil
}

func (r *MeetingRepo) SearchByKeyword(ctx context.Context, userID int64, keyword string) ([]Meeting, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, title, transcription, summary, created_at
		 FROM meetings WHERE user_id = $1 AND tsv @@ plainto_tsquery('russian', $2)
		 ORDER BY created_at DESC`,
		userID, keyword,
	)
	if err != nil {
		return nil, fmt.Errorf("search meetings: %w", err)
	}
	defer rows.Close()

	var meetings []Meeting
	for rows.Next() {
		var m Meeting
		if err := rows.Scan(&m.ID, &m.UserID, &m.Title, &m.Transcription, &m.Summary, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan meeting: %w", err)
		}
		meetings = append(meetings, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	return meetings, nil
}
