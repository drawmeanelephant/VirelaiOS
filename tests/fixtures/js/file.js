print("arithmetic", 1 + 2 * 3);
console.log("bigint", String(2n ** 80n));
print("math", Math.sin(Math.PI / 2), Math.log2(8), Object.is(Math.round(-0.5), -0));
print("json", JSON.stringify({ utf8: "é 🦋", sorted: [3, 1, 2].sort((a,b) => a-b) }));
print("regexp", /^(é)+$/u.test("éé"), "e\u0301".normalize("NFC"));
print("nul", "a\u0000b");
print("chunk", "x".repeat(300));
