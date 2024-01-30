package cachekey

const (
	SubscriptionKey  = "SUBSCRIPTION:"
	SubscribedKey    = "SUBSCRIBED:"
	UserStateConnKey = "USER_STATE_CONN:"
	GroupStateKey    = "GROUP_ONLINE:"
	GroupStateTagKey = "GROUP_ONLINE_TAG:"
)

func GetSubscriptionKey(userID string) string {
	return SubscriptionKey + userID
}

func GetSubscribedKey(userID string) string {
	return SubscribedKey + userID
}

func GetUserStateConnKey(userID string) string {
	return UserStateConnKey + userID
}

func GetGroupStateKey(groupID string) string {
	return GroupStateKey + groupID
}

func GetGroupStateTagKey(groupID string) string {
	return GroupStateTagKey + groupID
}
