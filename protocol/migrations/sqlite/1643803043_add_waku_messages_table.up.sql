CREATE TABLE waku_messages (
  sig BLOB NOT NULL,
  timestamp INT NOT NULL,
  topic TEXT NOT NULL,
  payload BLOB NOT NULL,
  padding BLOB NOT NULL,
  hash BLOB NOT NULL
);
