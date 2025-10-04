// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const std = @import("std");

const alloc = @import("allocator.zig");
const res_attrs = @import("resource_attributes.zig");
const test_util = @import("test_util.zig");

const testing = std.testing;

// Note on having tests embedded in the actual source files versus having them in a separate *_test.zig file: Proper
// pure unit tests are usually directly in the source file of the production function they are testing. More invasive
// tests that need to change the environment variables (for example) should go in a separate file, so we never run the
// risk of even compiling the test mechanism to modify the environment.

test "getModifiedOtelResourceAttributesValue: no original value, no new resource attributes (null)" {
    const original_environ = try test_util.clearStdCEnviron();
    defer test_util.resetStdCEnviron(original_environ);
    const modified_value = try res_attrs.getModifiedOtelResourceAttributesValue(null);
    try test_util.expectWithMessage(modified_value == null, "modified_value == null");
}

test "getModifiedOtelResourceAttributesValue: no original value, no new resource attributes (empty string)" {
    const original_environ = try test_util.clearStdCEnviron();
    defer test_util.resetStdCEnviron(original_environ);
    const modified_value = try res_attrs.getModifiedOtelResourceAttributesValue("");
    try test_util.expectWithMessage(modified_value == null, "modified_value == null");
}

test "getModifiedOtelResourceAttributesValue: no original value (null), new resource attributes: namespace only" {
    const original_environ = try test_util.setStdCEnviron(&[1][]const u8{"DASH0_NAMESPACE_NAME=namespace"});
    defer test_util.resetStdCEnviron(original_environ);
    const modified_value = try res_attrs.getModifiedOtelResourceAttributesValue(null) orelse return error.Unexpected;
    try testing.expectEqualStrings("k8s.namespace.name=namespace", std.mem.span(modified_value));
}

test "getModifiedOtelResourceAttributesValue: no original value (null), new resource attributes: pod name only" {
    const original_environ = try test_util.setStdCEnviron(&[1][]const u8{"DASH0_POD_NAME=pod"});
    defer test_util.resetStdCEnviron(original_environ);
    const modified_value = try res_attrs.getModifiedOtelResourceAttributesValue(null) orelse return error.Unexpected;
    try testing.expectEqualStrings("k8s.pod.name=pod", std.mem.span(modified_value));
}

test "getModifiedOtelResourceAttributesValue: no original value (null), new resource attributes: pod uid only" {
    const original_environ = try test_util.setStdCEnviron(&[1][]const u8{"DASH0_POD_UID=uid"});
    defer test_util.resetStdCEnviron(original_environ);
    const modified_value = try res_attrs.getModifiedOtelResourceAttributesValue(null) orelse return error.Unexpected;
    try testing.expectEqualStrings("k8s.pod.uid=uid", std.mem.span(modified_value));
}

test "getModifiedOtelResourceAttributesValue: no original value (null), new resource attributes: container name only" {
    const original_environ = try test_util.setStdCEnviron(&[1][]const u8{"DASH0_CONTAINER_NAME=container"});
    defer test_util.resetStdCEnviron(original_environ);
    const modified_value = try res_attrs.getModifiedOtelResourceAttributesValue(null) orelse return error.Unexpected;
    try testing.expectEqualStrings("k8s.container.name=container", std.mem.span(modified_value));
}

test "getModifiedOtelResourceAttributesValue: no original value (null), new resource attributes: DASH0_RESOURCE_ATTRIBUTES only" {
    const original_environ = try test_util.setStdCEnviron(&[1][]const u8{"DASH0_RESOURCE_ATTRIBUTES=aaa=bbb,ccc=ddd"});
    defer test_util.resetStdCEnviron(original_environ);
    const modified_value = try res_attrs.getModifiedOtelResourceAttributesValue(null) orelse return error.Unexpected;
    try testing.expectEqualStrings("aaa=bbb,ccc=ddd", std.mem.span(modified_value));
}

test "getModifiedOtelResourceAttributesValue: no original value (null), several new resource attributes" {
    const original_environ = try test_util.setStdCEnviron(&[3][]const u8{
        "DASH0_NAMESPACE_NAME=namespace",
        "DASH0_POD_NAME=pod",
        "DASH0_POD_UID=uid",
    });
    defer test_util.resetStdCEnviron(original_environ);
    const modified_value = try res_attrs.getModifiedOtelResourceAttributesValue(null) orelse return error.Unexpected;
    try testing.expectEqualStrings("k8s.namespace.name=namespace,k8s.pod.name=pod,k8s.pod.uid=uid", std.mem.span(modified_value));
}

test "getModifiedOtelResourceAttributesValue: no original value (empty string), several new resource attributes" {
    const original_environ = try test_util.setStdCEnviron(&[3][]const u8{
        "DASH0_NAMESPACE_NAME=namespace",
        "DASH0_POD_NAME=pod",
        "DASH0_POD_UID=uid",
    });
    defer test_util.resetStdCEnviron(original_environ);
    const modified_value = try res_attrs.getModifiedOtelResourceAttributesValue("") orelse return error.Unexpected;
    try testing.expectEqualStrings("k8s.namespace.name=namespace,k8s.pod.name=pod,k8s.pod.uid=uid", std.mem.span(modified_value));
}

test "getModifiedOtelResourceAttributesValue: no original value (null), new resource attributes: everything is set" {
    const original_environ = try test_util.setStdCEnviron(&[8][]const u8{
        "DASH0_NAMESPACE_NAME=namespace",
        "DASH0_POD_NAME=pod",
        "DASH0_POD_UID=uid",
        "DASH0_CONTAINER_NAME=container",
        "DASH0_SERVICE_NAME=service",
        "DASH0_SERVICE_VERSION=version",
        "DASH0_SERVICE_NAMESPACE=servicenamespace",
        "DASH0_RESOURCE_ATTRIBUTES=aaa=bbb,ccc=ddd",
    });
    defer test_util.resetStdCEnviron(original_environ);
    const modified_value = try res_attrs.getModifiedOtelResourceAttributesValue(null) orelse return error.Unexpected;
    try testing.expectEqualStrings(
        "aaa=bbb,ccc=ddd,k8s.namespace.name=namespace,k8s.pod.name=pod,k8s.pod.uid=uid,k8s.container.name=container,service.name=service,service.version=version,service.namespace=servicenamespace",
        std.mem.span(modified_value),
    );
}

test "getModifiedOtelResourceAttributesValue: original value exists, no new resource attributes" {
    const original_environ = try test_util.clearStdCEnviron();
    defer test_util.resetStdCEnviron(original_environ);
    const modified_value = try res_attrs.getModifiedOtelResourceAttributesValue("aaa=bbb,ccc=ddd");
    try test_util.expectWithMessage(modified_value == null, "modified_value == null");
}

test "getModifiedOtelResourceAttributesValue: original value and new resource attributes" {
    const original_environ = try test_util.setStdCEnviron(&[3][]const u8{
        "DASH0_NAMESPACE_NAME=namespace",
        "DASH0_POD_NAME=pod",
        "DASH0_POD_UID=uid",
    });
    defer test_util.resetStdCEnviron(original_environ);
    const modified_value = try res_attrs.getModifiedOtelResourceAttributesValue("aaa=bbb,ccc=ddd") orelse return error.Unexpected;
    try testing.expectEqualStrings("aaa=bbb,ccc=ddd,k8s.namespace.name=namespace,k8s.pod.name=pod,k8s.pod.uid=uid", std.mem.span(modified_value));
}

test "getModifiedOtelResourceAttributesValue: key-value pairs in original value have higher precedence than DASH0_*" {
    const original_environ = try test_util.setStdCEnviron(&[7][]const u8{
        "DASH0_NAMESPACE_NAME=namespace-dash0",
        "DASH0_POD_NAME=pod-dash0",
        "DASH0_POD_UID=uid-dash0",
        "DASH0_CONTAINER_NAME=container-dash0",
        "DASH0_SERVICE_NAME=service-dash0",
        "DASH0_SERVICE_VERSION=version-dash0",
        "DASH0_SERVICE_NAMESPACE=servicenamespace-dash0",
    });
    defer test_util.resetStdCEnviron(original_environ);
    const modified_value = try res_attrs.getModifiedOtelResourceAttributesValue("aaa=bbb,ccc=ddd,k8s.namespace.name=namespace-original,k8s.pod.name=pod-original,service.name=service-original,service.version=version-original,service.namespace=servicenamespace-original") orelse return error.Unexpected;
    try testing.expectEqualStrings("aaa=bbb,ccc=ddd,k8s.namespace.name=namespace-original,k8s.pod.name=pod-original,service.name=service-original,service.version=version-original,service.namespace=servicenamespace-original,k8s.pod.uid=uid-dash0,k8s.container.name=container-dash0", std.mem.span(modified_value));
}

test "getModifiedOtelResourceAttributesValue: key-value pairs in original value have higher precedence than DASH0_RESOURCE_ATTRIBUTES" {
    const original_environ = try test_util.setStdCEnviron(&[1][]const u8{
        "DASH0_RESOURCE_ATTRIBUTES=k8s.namespace.name=namespace-dash0,k8s.pod.name=pod-dash0,service.name=service-dash0,k8s.pod.uid=uid-dash0,k8s.container.name=container-dash0",
    });
    defer test_util.resetStdCEnviron(original_environ);
    const modified_value = try res_attrs.getModifiedOtelResourceAttributesValue("aaa=bbb,ccc=ddd,k8s.namespace.name=namespace-original,k8s.pod.name=pod-original,service.name=service-original,service.version=version-original,service.namespace=servicenamespace-original") orelse return error.Unexpected;
    try testing.expectEqualStrings("aaa=bbb,ccc=ddd,k8s.namespace.name=namespace-original,k8s.pod.name=pod-original,service.name=service-original,service.version=version-original,service.namespace=servicenamespace-original,k8s.pod.uid=uid-dash0,k8s.container.name=container-dash0", std.mem.span(modified_value));
}

test "getModifiedOtelResourceAttributesValue: DASH0_RESOURCE_ATTRIBUTES key-value pairs have higher precedence than other DASH0_* attributes" {
    const original_environ = try test_util.setStdCEnviron(&[8][]const u8{
        "DASH0_NAMESPACE_NAME=namespace-dash0_*",
        "DASH0_POD_NAME=pod-dash0_*",
        "DASH0_POD_UID=uid-dash0_*",
        "DASH0_CONTAINER_NAME=container-dash0_*",
        "DASH0_RESOURCE_ATTRIBUTES=k8s.namespace.name=namespace-dra,k8s.pod.name=pod-dra,service.name=service-dra,k8s.pod.uid=uid-dra,k8s.container.name=container-dra",
        "DASH0_SERVICE_NAME=service-dash0_*",
        "DASH0_SERVICE_VERSION=version-dash0_*",
        "DASH0_SERVICE_NAMESPACE=servicenamespace-dash0_*",
    });
    defer test_util.resetStdCEnviron(original_environ);
    const modified_value = try res_attrs.getModifiedOtelResourceAttributesValue(null) orelse return error.Unexpected;
    try testing.expectEqualStrings("k8s.namespace.name=namespace-dra,k8s.pod.name=pod-dra,service.name=service-dra,k8s.pod.uid=uid-dra,k8s.container.name=container-dra,service.version=version-dash0_*,service.namespace=servicenamespace-dash0_*", std.mem.span(modified_value));
}

test "getModifiedOtelResourceAttributesValue: mixing key-value pairs from the original value, DASH0_RESOURCE_ATTRIBUTES and DASH0_*" {
    const original_environ = try test_util.setStdCEnviron(&[8][]const u8{
        "DASH0_NAMESPACE_NAME=namespace-dash0_*",
        "DASH0_POD_NAME=pod-dash0_*",
        "DASH0_POD_UID=uid-dash0_*",
        "DASH0_CONTAINER_NAME=container-dash0_*",
        "DASH0_RESOURCE_ATTRIBUTES=k8s.namespace.name=namespace-dra,service.name=service-dra,k8s.pod.name=pod-dra",
        "DASH0_SERVICE_NAME=service-dash0_*",
        "DASH0_SERVICE_VERSION=version-dash0_*",
        "DASH0_SERVICE_NAMESPACE=servicenamespace-dash0_*",
    });
    defer test_util.resetStdCEnviron(original_environ);
    const modified_value = try res_attrs.getModifiedOtelResourceAttributesValue("k8s.namespace.name=namespace-original,service.name=service-original") orelse return error.Unexpected;
    try testing.expectEqualStrings("k8s.namespace.name=namespace-original,service.name=service-original,k8s.pod.name=pod-dra,k8s.pod.uid=uid-dash0_*,k8s.container.name=container-dash0_*,service.version=version-dash0_*,service.namespace=servicenamespace-dash0_*", std.mem.span(modified_value));
}

test "getModifiedOtelResourceAttributesValue: trims keys and drops empty DASH0_RESOURCE_ATTRIBUTES key-value pairs" {
    const original_environ = try test_util.setStdCEnviron(&[1][]const u8{"DASH0_RESOURCE_ATTRIBUTES=aaa=bbb,,  ccc=ddd,  , eee = fff "});
    defer test_util.resetStdCEnviron(original_environ);
    const modified_value = try res_attrs.getModifiedOtelResourceAttributesValue(null) orelse return error.Unexpected;
    try testing.expectEqualStrings("aaa=bbb,ccc=ddd,eee= fff ", std.mem.span(modified_value));
}
