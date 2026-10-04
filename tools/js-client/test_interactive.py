import unittest
from interactive import receipt_batch, observed_marker


class ReceiptBatchTests(unittest.TestCase):
    def test_batches_stay_below_native_tty_capacity_and_eval_ceiling(self):
        for before in range(256):
            after, payload = receipt_batch(before)
            self.assertGreater(after, before)
            self.assertLessEqual(after, 256)
            self.assertEqual(payload, b"undefined\n" * (after - before))
            self.assertLessEqual(len(payload), 160)
        for before in (-1, 256):
            with self.assertRaisesRegex(ValueError, "ReceiptBatchEvalLimit"):
                receipt_batch(before)

    def test_receipt_refusal_wins_over_ready_marker(self):
        observed = b"qjs: ready eval=32\nqjs: ReceiptLimit\nqjs: clean\n"
        self.assertEqual(observed_marker(observed, "qjs: ready eval=32\n",
                                        alternative="qjs: ReceiptLimit"), "qjs: ReceiptLimit")

    def test_cleanup_does_not_prevent_waiting_for_reap(self):
        self.assertIsNone(observed_marker(b"qjs: clean\n", "tasks user-exec reaped"))
        self.assertEqual(observed_marker(b"qjs: clean\ntasks user-exec reaped\n",
                                         "tasks user-exec reaped"), "tasks user-exec reaped")

    def test_readiness_after_exit_or_native_fault_is_not_success(self):
        with self.assertRaisesRegex(RuntimeError, "guest exited"):
            observed_marker(b"qjs: clean\n", "qjs: ready eval=33\n")
        with self.assertRaisesRegex(RuntimeError, "guest QJS.BIN fault"):
            observed_marker(b"fault: QJS.BIN\nqjs: ready eval=33\n", "qjs: ready eval=33\n")


if __name__ == "__main__":
    unittest.main()
