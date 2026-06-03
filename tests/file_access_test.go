package tests

import (
	"context"
	"errors"
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

func TestAIParseSourceDeleteEligibility(t *testing.T) {
	pool := newPostgresIntegrationPool(t)
	repos := repository.New(pool)
	ctx := context.Background()

	seedAIParseSourceRetentionFixtures(t, pool)

	assertAIParseSourceDeleteAllowed(t, repos, aiSourceDeleteFile, true, "source_deleted_after_parse-only AI source can be deleted")
	assertAIParseSourceDeleteAllowed(t, repos, aiConversationRetainedFile, false, "default conversation retention keeps AI source")
	assertAIParseSourceDeleteAllowed(t, repos, aiSharedChatFile, false, "non-AI business references keep AI source")

	if err := repos.Files.SoftDelete(ctx, aiSourceDeleteFile); err != nil {
		t.Fatalf("soft delete AI source: %v", err)
	}
	assertAIParseSourceDeleteAllowed(t, repos, aiSourceDeleteFile, false, "already deleted source is not eligible")
}

func TestExpiredTemporaryAIUploadedFileCleanup(t *testing.T) {
	pool := newPostgresIntegrationPool(t)
	repos := repository.New(pool)
	ctx := context.Background()

	seedExpiredTemporaryAIFileFixtures(t, pool)

	candidates, err := repos.AI.CleanupExpiredTemporaryUploadedFiles(ctx, 10)
	if err != nil {
		t.Fatalf("CleanupExpiredTemporaryUploadedFiles() error = %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("cleaned candidates = %d, want 1", len(candidates))
	}
	if candidates[0].AIFileID != aiExpiredTemporaryUploadID {
		t.Fatalf("cleaned AI file ID = %s, want %s", candidates[0].AIFileID, aiExpiredTemporaryUploadID)
	}
	if candidates[0].ExtractedTextFileID == nil || *candidates[0].ExtractedTextFileID != aiExpiredExtractedTextFile {
		t.Fatalf("cleaned extracted text file ID = %v, want %s", candidates[0].ExtractedTextFileID, aiExpiredExtractedTextFile)
	}

	assertAIUploadedFileDeleted(t, pool, aiExpiredTemporaryUploadID, true)
	assertAIUploadedFileDeleted(t, pool, aiActiveTemporaryUploadID, false)
	assertAIUploadedFileDeleted(t, pool, aiConversationRetentionUploadID, false)
	assertAIFileUsageDeleted(t, pool, aiExpiredTemporaryUploadID, true)
	assertAIFileUsageDeleted(t, pool, aiActiveTemporaryUploadID, false)
	assertAIFileChunkCount(t, pool, aiExpiredTemporaryUploadID, 0)
	assertAIFileChunkCount(t, pool, aiActiveTemporaryUploadID, 1)
}

func TestDeleteAIConversationReleasesFilesAndChunks(t *testing.T) {
	pool := newPostgresIntegrationPool(t)
	repos := repository.New(pool)
	ctx := context.Background()

	seedExpiredTemporaryAIFileFixtures(t, pool)

	if err := repos.AI.DeleteConversation(ctx, aiTempOwnerID, aiTemporaryConversationID); err != nil {
		t.Fatalf("DeleteConversation() error = %v", err)
	}

	assertAIConversationDeleted(t, pool, aiTemporaryConversationID, true)
	assertAIUploadedFileDeleted(t, pool, aiExpiredTemporaryUploadID, true)
	assertAIUploadedFileDeleted(t, pool, aiActiveTemporaryUploadID, true)
	assertAIUploadedFileDeleted(t, pool, aiConversationRetentionUploadID, true)
	assertAIFileUsageDeleted(t, pool, aiExpiredTemporaryUploadID, true)
	assertAIFileUsageDeleted(t, pool, aiActiveTemporaryUploadID, true)
	assertAIFileUsageDeleted(t, pool, aiConversationRetentionUploadID, true)
	assertAIFileChunkCount(t, pool, aiExpiredTemporaryUploadID, 0)
	assertAIFileChunkCount(t, pool, aiActiveTemporaryUploadID, 0)
	assertAIFileChunkCount(t, pool, aiConversationRetentionUploadID, 0)

	if err := repos.AI.DeleteConversation(ctx, aiTempOwnerID, aiTemporaryConversationID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("second DeleteConversation() error = %v, want ErrNotFound", err)
	}
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

func assertAIParseSourceDeleteAllowed(t *testing.T, repos repository.Repositories, fileID string, want bool, label string) {
	t.Helper()

	got, err := repos.Files.CanDeleteAIParseSourceAfterParse(context.Background(), fileID)
	if err != nil {
		t.Fatalf("%s: CanDeleteAIParseSourceAfterParse() error = %v", label, err)
	}
	if got != want {
		t.Fatalf("%s: allowed = %v, want %v", label, got, want)
	}
}

func assertAIUploadedFileDeleted(t *testing.T, pool *pgxpool.Pool, aiFileID string, want bool) {
	t.Helper()

	var got bool
	if err := pool.QueryRow(context.Background(), `
		SELECT deleted_at IS NOT NULL
		FROM ai_uploaded_files
		WHERE id = $1::uuid`,
		aiFileID,
	).Scan(&got); err != nil {
		t.Fatalf("read AI uploaded file deleted state: %v", err)
	}
	if got != want {
		t.Fatalf("AI uploaded file %s deleted = %v, want %v", aiFileID, got, want)
	}
}

func assertAIConversationDeleted(t *testing.T, pool *pgxpool.Pool, conversationID string, want bool) {
	t.Helper()

	var got bool
	if err := pool.QueryRow(context.Background(), `
		SELECT deleted_at IS NOT NULL
		FROM ai_conversations
		WHERE id = $1::uuid`,
		conversationID,
	).Scan(&got); err != nil {
		t.Fatalf("read AI conversation deleted state: %v", err)
	}
	if got != want {
		t.Fatalf("AI conversation %s deleted = %v, want %v", conversationID, got, want)
	}
}

func assertAIFileUsageDeleted(t *testing.T, pool *pgxpool.Pool, businessID string, want bool) {
	t.Helper()

	var got bool
	if err := pool.QueryRow(context.Background(), `
		SELECT bool_and(deleted_at IS NOT NULL)
		FROM file_usages
		WHERE business_id = $1::uuid
		  AND usage_type IN ('ai_upload', 'ai_extracted_text')`,
		businessID,
	).Scan(&got); err != nil {
		t.Fatalf("read AI file usage deleted state: %v", err)
	}
	if got != want {
		t.Fatalf("AI file usages for %s deleted = %v, want %v", businessID, got, want)
	}
}

func assertAIFileChunkCount(t *testing.T, pool *pgxpool.Pool, aiFileID string, want int) {
	t.Helper()

	var got int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*)
		FROM ai_file_chunks
		WHERE ai_file_id = $1::uuid`,
		aiFileID,
	).Scan(&got); err != nil {
		t.Fatalf("read AI file chunk count: %v", err)
	}
	if got != want {
		t.Fatalf("AI file chunks for %s = %d, want %d", aiFileID, got, want)
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

func seedAIParseSourceRetentionFixtures(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, public_id, email, password_hash, nick)
		VALUES ($1::uuid, 'ai_owner', 'ai-owner@example.test', 'hash', 'AI Owner');

		INSERT INTO files (id, owner_user_id, original_file_name, bucket, object_key, mime_type, size_bytes, storage_status, visibility)
		VALUES
		  ($2::uuid, $1::uuid, 'delete-me.txt', 'netherlink-private', 'delete-me.txt', 'text/plain', 10, 'ready', 'private'),
		  ($3::uuid, $1::uuid, 'keep-me.txt', 'netherlink-private', 'keep-me.txt', 'text/plain', 10, 'ready', 'private'),
		  ($4::uuid, $1::uuid, 'shared.txt', 'netherlink-private', 'shared.txt', 'text/plain', 10, 'ready', 'private');

		INSERT INTO ai_conversations (id, user_id, title)
		VALUES ($5::uuid, $1::uuid, 'AI');
		INSERT INTO ai_uploaded_files (id, conversation_id, file_id, purpose, parse_status, retention_policy)
		VALUES
		  ($6::uuid, $5::uuid, $2::uuid, 'reference', 'ready', 'source_deleted_after_parse'),
		  ($7::uuid, $5::uuid, $3::uuid, 'reference', 'ready', 'conversation'),
		  ($8::uuid, $5::uuid, $4::uuid, 'reference', 'ready', 'source_deleted_after_parse');
		INSERT INTO file_usages (file_id, usage_type, business_id, owner_user_id, visibility)
		VALUES
		  ($2::uuid, 'ai_upload', $6::uuid, $1::uuid, 'private'),
		  ($3::uuid, 'ai_upload', $7::uuid, $1::uuid, 'private'),
		  ($4::uuid, 'ai_upload', $8::uuid, $1::uuid, 'private');

		INSERT INTO conversations (id, type)
		VALUES ($9::uuid, 'direct');
		INSERT INTO messages (id, conversation_id, sender_id, message_seq, type, client_message_id)
		VALUES ($10::uuid, $9::uuid, $1::uuid, 1, 'file', 'client-shared-ai-source');
		INSERT INTO message_attachments (message_id, file_id, display_name)
		VALUES ($10::uuid, $4::uuid, 'shared.txt');
		`,
		aiOwnerID,
		aiSourceDeleteFile,
		aiConversationRetainedFile,
		aiSharedChatFile,
		aiConversationID,
		aiSourceDeleteUploadID,
		aiConversationRetainedUploadID,
		aiSharedChatUploadID,
		aiSharedConversationID,
		aiSharedMessageID,
	)
	if err != nil {
		t.Fatalf("seed AI parse source retention fixtures: %v", err)
	}
}

func seedExpiredTemporaryAIFileFixtures(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, public_id, email, password_hash, nick)
		VALUES ($1::uuid, 'ai_temp_owner', 'ai-temp-owner@example.test', 'hash', 'AI Temp Owner');

		INSERT INTO files (id, owner_user_id, original_file_name, bucket, object_key, mime_type, size_bytes, storage_status, visibility)
		VALUES
		  ($2::uuid, $1::uuid, 'expired.txt', 'netherlink-private', 'expired.txt', 'text/plain', 10, 'ready', 'private'),
		  ($3::uuid, $1::uuid, 'active.txt', 'netherlink-private', 'active.txt', 'text/plain', 10, 'ready', 'private'),
		  ($4::uuid, $1::uuid, 'conversation.txt', 'netherlink-private', 'conversation.txt', 'text/plain', 10, 'ready', 'private'),
		  ($5::uuid, $1::uuid, 'expired.extracted.txt', 'netherlink-private', 'expired.extracted.txt', 'text/plain', 10, 'ready', 'private');

		INSERT INTO ai_conversations (id, user_id, title)
		VALUES ($6::uuid, $1::uuid, 'AI temp cleanup');
		INSERT INTO ai_uploaded_files (id, conversation_id, file_id, purpose, parse_status, extracted_text_file_id, retention_policy, expires_at)
		VALUES
		  ($7::uuid, $6::uuid, $2::uuid, 'reference', 'ready', $5::uuid, 'temporary', now() - interval '1 minute'),
		  ($8::uuid, $6::uuid, $3::uuid, 'reference', 'ready', NULL, 'temporary', now() + interval '1 hour'),
		  ($9::uuid, $6::uuid, $4::uuid, 'reference', 'ready', NULL, 'conversation', now() - interval '1 minute');
		INSERT INTO file_usages (file_id, usage_type, business_id, owner_user_id, visibility)
		VALUES
		  ($2::uuid, 'ai_upload', $7::uuid, $1::uuid, 'private'),
		  ($5::uuid, 'ai_extracted_text', $7::uuid, $1::uuid, 'private'),
		  ($3::uuid, 'ai_upload', $8::uuid, $1::uuid, 'private'),
		  ($4::uuid, 'ai_upload', $9::uuid, $1::uuid, 'private');
		INSERT INTO ai_file_chunks (ai_file_id, chunk_index, content, token_count)
		VALUES
		  ($7::uuid, 0, 'expired chunk', 3),
		  ($8::uuid, 0, 'active chunk', 3),
		  ($9::uuid, 0, 'conversation chunk', 3);
		`,
		aiTempOwnerID,
		aiExpiredTemporaryFile,
		aiActiveTemporaryFile,
		aiConversationRetentionFile,
		aiExpiredExtractedTextFile,
		aiTemporaryConversationID,
		aiExpiredTemporaryUploadID,
		aiActiveTemporaryUploadID,
		aiConversationRetentionUploadID,
	)
	if err != nil {
		t.Fatalf("seed expired temporary AI file fixtures: %v", err)
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

	aiOwnerID                      = "60000000-0000-0000-0000-000000000001"
	aiSourceDeleteFile             = "61000000-0000-0000-0000-000000000001"
	aiConversationRetainedFile     = "61000000-0000-0000-0000-000000000002"
	aiSharedChatFile               = "61000000-0000-0000-0000-000000000003"
	aiConversationID               = "62000000-0000-0000-0000-000000000001"
	aiSourceDeleteUploadID         = "63000000-0000-0000-0000-000000000001"
	aiConversationRetainedUploadID = "63000000-0000-0000-0000-000000000002"
	aiSharedChatUploadID           = "63000000-0000-0000-0000-000000000003"
	aiSharedConversationID         = "64000000-0000-0000-0000-000000000001"
	aiSharedMessageID              = "64000000-0000-0000-0000-000000000002"

	aiTempOwnerID                   = "70000000-0000-0000-0000-000000000001"
	aiExpiredTemporaryFile          = "71000000-0000-0000-0000-000000000001"
	aiActiveTemporaryFile           = "71000000-0000-0000-0000-000000000002"
	aiConversationRetentionFile     = "71000000-0000-0000-0000-000000000003"
	aiExpiredExtractedTextFile      = "71000000-0000-0000-0000-000000000004"
	aiTemporaryConversationID       = "72000000-0000-0000-0000-000000000001"
	aiExpiredTemporaryUploadID      = "73000000-0000-0000-0000-000000000001"
	aiActiveTemporaryUploadID       = "73000000-0000-0000-0000-000000000002"
	aiConversationRetentionUploadID = "73000000-0000-0000-0000-000000000003"
)
