DROP INDEX IF EXISTS chunked_messages_first_seen_at_idx;
DROP TABLE IF EXISTS chunked_message_chunks;
DROP TABLE IF EXISTS chunked_messages;

CREATE TABLE chunked_messages (
    message_id               VARCHAR(150)  NOT NULL PRIMARY KEY,
    stored_chunks_count      INTEGER       NOT NULL,
    expected_chunks_count    INTEGER       NOT NULL
);
