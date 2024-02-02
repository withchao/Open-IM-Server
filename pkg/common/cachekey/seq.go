package cachekey

const (
	MallocSeq     = "MALLOC_SEQ:"
	MallocSeqLock = "MALLOC_SEQ_LOCK:"
)

func GetMallocSeq(conversationID string) string {
	return MallocSeq + conversationID
}

func GetMallocSeqLock(conversationID string) string {
	return MallocSeqLock + conversationID
}
