package secretbox

import "strconv"

func NotificationChannelAAD(channelID string, revision int64, channelType string) ResourceAAD {
	return ResourceAAD{Domain: "beat", Namespace: "notification_channel_revision", RowID: channelID,
		Field: "secret_document", Revision: strconv.FormatInt(revision, 10) + "/" + channelType, PayloadVersion: "v1"}
}

func NotificationMessageAAD(messageID string) ResourceAAD {
	return ResourceAAD{Domain: "beat", Namespace: "notification_message", RowID: messageID, Field: "payload", PayloadVersion: "v1"}
}
