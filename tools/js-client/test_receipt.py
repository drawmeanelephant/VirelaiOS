import unittest
from receipt import parse, output, require_failure_tail


class ReceiptTests(unittest.TestCase):
    def test_binary_and_empty_frames(self):
        frames = parse(b"H 6\nQJS/1\nO 4\na\x00b\nO 0\nZ 9\nstatus=0\n")
        self.assertEqual(output(frames), b"a\x00b\n")

    def test_partial_or_invalid_receipts_fail(self):
        for data in (b"", b"H 6\nQJS/1\n", b"H 99\nx", b"X 0\n"):
            with self.assertRaises((ValueError, IndexError)):
                parse(data)

    def test_failed_new_receipt_requires_explicit_partial_mode(self):
        data = b"H 6\nQJS/1\nO 2\nx\n"
        with self.assertRaises(ValueError):
            parse(data)
        self.assertEqual(output(parse(data, require_complete=False)), b"x\n")
        with self.assertRaises(ValueError):
            parse(b"H 6\nQJS/1\nO 3\nx", require_complete=False)

    def test_failed_receipt_may_fit_a_non_success_terminal_frame(self):
        complete = parse(b"H 6\nQJS/1\nZ 10\nstatus=70\n")
        require_failure_tail(complete)
        partial = parse(b"H 6\nQJS/1\nO 2\nx\n", require_complete=False)
        require_failure_tail(partial)
        with self.assertRaisesRegex(ValueError, "FailureReceiptStatus"):
            require_failure_tail(parse(b"H 6\nQJS/1\nZ 9\nstatus=0\n"))


if __name__ == "__main__":
    unittest.main()
