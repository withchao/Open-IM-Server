package cachekey

const (
	SubscriptionKey  = "SUBSCRIPTION:"
	SubscribedKey    = "SUBSCRIBED:"
	UserStateConnKey = "USER_STATE_CONN:"
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
