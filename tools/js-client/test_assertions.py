import unittest
from assertions import last_scheduler_reap


class ReapTests(unittest.TestCase):
    def test_receipt_reaped_field_is_not_a_scheduler_marker(self):
        text = "tasks user-exec reaped\npages: armed=1\nruntime-receipt: reaped=1\n"
        self.assertEqual(last_scheduler_reap(text), 0)
        self.assertEqual(last_scheduler_reap("runtime-receipt: reaped=1\n"), -1)

    def test_last_real_reap_remains_required_before_snapshot(self):
        text = "tasks user-exec reaped\npages: armed=1\ntasks user-exec reaped\n"
        self.assertGreater(last_scheduler_reap(text), text.index("pages: armed=1"))


if __name__ == "__main__":
    unittest.main()
