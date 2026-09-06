package handler

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

// Table names are internal constants, never supplied by a request.
func validateRelations(ctx context.Context, tx pgx.Tx, userID string, chapter *int, moods, collections []int) error {
	groups := []struct {
		table string
		ids   []int
	}{{"moods", moods}, {"collections", collections}}
	if chapter != nil {
		groups = append(groups, struct {
			table string
			ids   []int
		}{"chapters", []int{*chapter}})
	}
	for _, group := range groups {
		if len(group.ids) == 0 {
			continue
		}
		unique := map[int]bool{}
		for _, id := range group.ids {
			if id <= 0 {
				return errors.New("invalid related item")
			}
			unique[id] = true
		}
		var count int
		if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM "+group.table+" WHERE user_id = $1 AND id = ANY($2)", userID, group.ids).Scan(&count); err != nil {
			return err
		}
		if count != len(unique) {
			return errors.New("related item not found in this account")
		}
	}
	return nil
}
