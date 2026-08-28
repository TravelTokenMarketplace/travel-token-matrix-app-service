-- Track WHICH chunk indices arrived, not merely how many arrivals there were.
--
-- Counting arrivals cannot tell a redelivered chunk from a new one, so on a
-- lossy link a duplicate could complete a message that still had a hole in it,
-- and could push the arrival count past the expected count and mark a
-- compliant sender as misbehaving.
--
-- chunked_messages keeps only what is true of the message as a whole; the set
-- of received indices lives in chunked_message_chunks, one row per distinct
-- index, so a redelivery is an ignored duplicate insert rather than a count.
--
-- expected_chunks_count is 0 until the signed message that carries it arrives,
-- which lets a chunk that overtakes it be recorded rather than rejected.
-- first_seen_at (unix seconds) exists so partial messages can be swept.
--
-- sender is recorded per chunk, not per message, because a message id is
-- chosen by the sender and nothing binds one to a single account. An index
-- that turns out to be out of range can only be judged once the count is
-- declared, which may be a later event from a different account, so the row
-- has to remember who actually sent it or the wrong peer gets blamed.
--
-- The old table only ever held in-flight reassembly tracking, so it is
-- replaced rather than converted: anything in it is a message that was still
-- arriving, and those resolve by being re-sent or swept.

DROP TABLE IF EXISTS chunked_messages;

CREATE TABLE chunked_messages (
    message_id               VARCHAR(150)  NOT NULL PRIMARY KEY,
    expected_chunks_count    INTEGER       NOT NULL DEFAULT 0,
    first_seen_at            INTEGER       NOT NULL
);

CREATE TABLE chunked_message_chunks (
    message_id               VARCHAR(150)  NOT NULL,
    chunk_index              INTEGER       NOT NULL,
    sender                   VARCHAR(255)  NOT NULL,
    PRIMARY KEY (message_id, chunk_index)
);

CREATE INDEX chunked_messages_first_seen_at_idx ON chunked_messages (first_seen_at);
