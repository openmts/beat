package secretbox

import "strconv"

func OperationSnippetAAD(id string, version int, shell string) ResourceAAD {
	return ResourceAAD{Domain: "beat", Namespace: "operation_snippet", RowID: id,
		Field: "content", Revision: strconv.Itoa(version) + ":" + shell, PayloadVersion: "v1"}
}

func OperationTaskAAD(id string) ResourceAAD {
	return ResourceAAD{Domain: "beat", Namespace: "operation_task", RowID: id, Field: "payload", PayloadVersion: "v1"}
}

func OperationOutputAAD(id, field string) ResourceAAD {
	return ResourceAAD{Domain: "beat", Namespace: "operation_attempt", RowID: id, Field: field, PayloadVersion: "v1"}
}

func OperationTransferAAD(id string) ResourceAAD {
	return ResourceAAD{Domain: "beat", Namespace: "operation_transfer", RowID: id, Field: "path", PayloadVersion: "v1"}
}

func OperationArtifactAAD(id string) ResourceAAD {
	return ResourceAAD{Domain: "beat", Namespace: "operation_artifact", RowID: id, Field: "metadata", PayloadVersion: "v1"}
}
