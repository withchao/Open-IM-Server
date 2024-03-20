package cachekey

const (
	SeqUserReadSeq     = "SEQ_USER_READ_SEQ:"
	SeqUserReadLockSeq = "SEQ_USER_READ_LOCK_SEQ:"
	SeqUserMaxSeq      = "SEQ_USER_MAX_SEQ:"
	SeqUserMinSeq      = "SEQ_USER_MIN_SEQ:"
)

func GetSeqUserReadSeqKey(conversationID string, userID string) string {
	return SeqUserReadSeq + conversationID + ":" + userID
}

func GetSeqUserReadLockSeqKey(conversationID string, userID string) string {
	return SeqUserReadLockSeq + conversationID + ":" + userID
}

func GetSeqUserMaxSeqKey(conversationID string, userID string) string {
	return SeqUserMaxSeq + conversationID + ":" + userID
}

func GetSeqUserMinSeqKey(conversationID string, userID string) string {
	return SeqUserMinSeq + conversationID + ":" + userID
}
