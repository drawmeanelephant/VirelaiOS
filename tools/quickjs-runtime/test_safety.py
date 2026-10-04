import unittest
from safety import instrument_text


class SafetyTests(unittest.TestCase):
    def test_nested_braces_unbraced_loops_and_dangling_else(self):
        source = """static int f(int n) {
        for (int i=0;i<n;i++) if (n) while(n--) n--; else n++;
        do if(n) n--; else n++; while(n);
        while(n) { for(;;) break; }
        return n; }"""
        result, receipt = instrument_text(source)
        self.assertEqual(len(receipt["entries"]), 1)
        self.assertEqual(len(receipt["loops"]), 5)
        self.assertEqual(result.count("qjs_native_step();"), 6)
        self.assertIn("else n++; }", result)
        self.assertNotIn("while {", result)

    def test_strings_and_global_function_pointer_initializers_are_not_bodies(self):
        source = 'typedef int (*Op)(int); Op table[]={f}; const char *s="while(1){}"; int f(int n){while(n--){}return 0;}'
        result, receipt = instrument_text(source)
        self.assertEqual([row["function"] for row in receipt["entries"]], ["f"])
        self.assertEqual(len(receipt["loops"]), 1)
        self.assertIn('"while(1){}"', result)

    def test_missing_or_unbalanced_coverage_is_a_failure(self):
        for source in ("int f(void){return 1;}", "int f(void){while(1){"):
            with self.assertRaises(ValueError):
                instrument_text(source)


if __name__ == "__main__":
    unittest.main()
