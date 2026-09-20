package runtime

func testResourceDefinition[In, Out, Config any]() ResourceDefinition[In, Out, Config] {
	return ResourceDefinition[In, Out, Config]{SchemaVersion: 1}
}
