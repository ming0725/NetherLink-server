package tests

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"nlserver/internal/repository"
)

func TestFileAccessBusinessReferences(t *testing.T) {
	pool := newPostgresIntegrationPool(t)
	repos := repository.New(pool)
	ctx := context.Background()

	seedFileAccessFixtures(t, pool)

	assertFileAccess(t, repos, userMember, fileChatAttachment, true, "conversation participant can access chat attachment")
	assertFileAccess(t, repos, userOutsider, fileChatAttachment, false, "nonparticipant cannot access chat attachment")

	if _, err := pool.Exec(ctx, `
		UPDATE conversation_participants
		SET hidden_at = now() + interval '1 minute'
		WHERE conversation_id = $1::uuid AND user_id = $2::uuid`,
		conversationID,
		userMember,
	); err != nil {
		t.Fatalf("hide conversation history: %v", err)
	}
	assertFileAccess(t, repos, userMember, fileChatAttachment, false, "hidden history blocks old chat attachment")

	assertFileAccess(t, repos, userMember, fileGroupAvatar, true, "group member can access group avatar")
	assertFileAccess(t, repos, userOutsider, fileGroupAvatar, false, "nonmember cannot access group avatar")

	assertFileAccess(t, repos, userAuthor, filePrivatePostMedia, true, "private post author can access media")
	assertFileAccess(t, repos, userOutsider, filePrivatePostMedia, false, "private post hides media from other users")
	assertFileAccess(t, repos, userOutsider, filePublicPostMedia, true, "public post exposes media")
	assertFileAccess(t, repos, userOutsider, filePublicPostCover, true, "public post exposes cover media")
}

func newPostgresIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("NETHERLINK_TEST_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set NETHERLINK_TEST_POSTGRES_DSN to run PostgreSQL integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	adminPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect PostgreSQL test database: %v", err)
	}

	schema := fmt.Sprintf("nltest_%d", time.Now().UnixNano())
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = adminPool.Exec(cleanupCtx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE`)
		adminPool.Close()
	})

	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse PostgreSQL test DSN: %v", err)
	}
	poolConfig.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	if poolConfig.ConnConfig.RuntimeParams == nil {
		poolConfig.ConnConfig.RuntimeParams = map[string]string{}
	}
	poolConfig.ConnConfig.RuntimeParams["search_path"] = schema + ",public"

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatalf("connect PostgreSQL test schema: %v", err)
	}
	t.Cleanup(pool.Close)

	for _, script := range []string{"01_extensions.sql", "02_schema.sql"} {
		sql, err := os.ReadFile(testSchemaPath(t, script))
		if err != nil {
			t.Fatalf("read %s: %v", script, err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("apply %s: %v", script, err)
		}
	}

	return pool
}

func testSchemaPath(t *testing.T, name string) string {
	t.Helper()

	for _, root := range []string{".", ".."} {
		path := filepath.Join(root, "db", "init", name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	t.Fatalf("schema script %s not found", name)
	return ""
}

func assertFileAccess(t *testing.T, repos repository.Repositories, userID string, fileID string, want bool, label string) {
	t.Helper()

	got, err := repos.Files.CanUserAccessFile(context.Background(), userID, fileID)
	if err != nil {
		t.Fatalf("%s: CanUserAccessFile() error = %v", label, err)
	}
	if got != want {
		t.Fatalf("%s: allowed = %v, want %v", label, got, want)
	}
}

func seedFileAccessFixtures(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, public_id, email, password_hash, nick)
		VALUES
		  ($1::uuid, 'owner', 'owner@example.test', 'hash', 'Owner'),
		  ($2::uuid, 'member', 'member@example.test', 'hash', 'Member'),
		  ($3::uuid, 'outsider', 'outsider@example.test', 'hash', 'Outsider'),
		  ($4::uuid, 'author', 'author@example.test', 'hash', 'Author');

		INSERT INTO files (id, owner_user_id, original_file_name, bucket, object_key, mime_type, size_bytes, storage_status, visibility)
		VALUES
		  ($5::uuid, $1::uuid, 'chat.jpg', 'netherlink-protected', 'chat.jpg', 'image/jpeg', 10, 'ready', 'private'),
		  ($6::uuid, $1::uuid, 'group.jpg', 'netherlink-protected', 'group.jpg', 'image/jpeg', 10, 'ready', 'private'),
		  ($7::uuid, $4::uuid, 'private.jpg', 'netherlink-protected', 'private.jpg', 'image/jpeg', 10, 'ready', 'private'),
		  ($8::uuid, $4::uuid, 'public.jpg', 'netherlink-protected', 'public.jpg', 'image/jpeg', 10, 'ready', 'private'),
		  ($9::uuid, $4::uuid, 'cover.jpg', 'netherlink-protected', 'cover.jpg', 'image/jpeg', 10, 'ready', 'private');

		INSERT INTO conversations (id, type)
		VALUES ($10::uuid, 'direct');
		INSERT INTO conversation_participants (conversation_id, user_id)
		VALUES ($10::uuid, $1::uuid), ($10::uuid, $2::uuid);
		INSERT INTO messages (id, conversation_id, sender_id, message_seq, type, client_message_id)
		VALUES ($11::uuid, $10::uuid, $1::uuid, 1, 'image', 'client-chat-1');
		INSERT INTO message_attachments (message_id, file_id, display_name, width, height)
		VALUES ($11::uuid, $5::uuid, 'chat.jpg', 320, 240);

		INSERT INTO groups (id, name, owner_id, avatar_file_id)
		VALUES ($12::uuid, 'Group', $1::uuid, $6::uuid);
		INSERT INTO group_members (group_id, user_id, role)
		VALUES ($12::uuid, $2::uuid, 'member');

		INSERT INTO posts (id, author_id, content, visibility)
		VALUES
		  ($13::uuid, $4::uuid, 'private post', 'private'),
		  ($14::uuid, $4::uuid, 'public post', 'public');
		INSERT INTO post_media (id, post_id, file_id, media_type, sort_order)
		VALUES ($15::uuid, $13::uuid, $7::uuid, 'image', 0);
		INSERT INTO post_media (id, post_id, file_id, media_type, sort_order, cover_file_id)
		VALUES ($16::uuid, $14::uuid, $8::uuid, 'video', 0, $9::uuid);
		`,
		userOwner,
		userMember,
		userOutsider,
		userAuthor,
		fileChatAttachment,
		fileGroupAvatar,
		filePrivatePostMedia,
		filePublicPostMedia,
		filePublicPostCover,
		conversationID,
		messageID,
		groupID,
		privatePostID,
		publicPostID,
		privatePostMediaID,
		publicPostMediaID,
	)
	if err != nil {
		t.Fatalf("seed file access fixtures: %v", err)
	}
}

const (
	userOwner    = "00000000-0000-0000-0000-000000000001"
	userMember   = "00000000-0000-0000-0000-000000000002"
	userOutsider = "00000000-0000-0000-0000-000000000003"
	userAuthor   = "00000000-0000-0000-0000-000000000004"

	fileChatAttachment   = "10000000-0000-0000-0000-000000000001"
	fileGroupAvatar      = "10000000-0000-0000-0000-000000000002"
	filePrivatePostMedia = "10000000-0000-0000-0000-000000000003"
	filePublicPostMedia  = "10000000-0000-0000-0000-000000000004"
	filePublicPostCover  = "10000000-0000-0000-0000-000000000005"

	conversationID     = "20000000-0000-0000-0000-000000000001"
	messageID          = "20000000-0000-0000-0000-000000000002"
	groupID            = "30000000-0000-0000-0000-000000000001"
	privatePostID      = "40000000-0000-0000-0000-000000000001"
	publicPostID       = "40000000-0000-0000-0000-000000000002"
	privatePostMediaID = "50000000-0000-0000-0000-000000000001"
	publicPostMediaID  = "50000000-0000-0000-0000-000000000002"
)
