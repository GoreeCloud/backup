import unittest

from scripts.check_workflow_actions import examine_workflow


class ActionReferenceValidationTest(unittest.TestCase):
    def test_pinned_commit_refs_are_accepted(self):
        valid = """jobs:
  check:
    steps:
      - uses: actions/checkout@11d5960a326750d5838078e36cf38b85af677262 # v4
      - uses: gradle/actions/setup-gradle@ed408507eac070d1f99cc633dbcf757c94c7933a # v4.4.3
      - uses: ./.github/actions/local-check
"""
        count, errors = examine_workflow(valid)
        self.assertEqual(count, 2)
        self.assertEqual(errors, [])

    def test_floating_and_truncated_refs_fail(self):
        source = """steps:
 - uses: actions/upload-artifact@v4
 - uses: actions/setup-node@49933e
 - uses: example/action@main
 - uses: docker://example.com/image:latest
"""
        count, errors = examine_workflow(source)
        self.assertEqual(count, 4)
        self.assertEqual(len(errors), 4)

    def test_comments_cannot_hide_a_floating_tag(self):
        count, errors = examine_workflow(
            "    uses: actions/checkout@v4 # pinned elsewhere\n"
            "    # uses: actions/checkout@v4\n"
        )
        self.assertEqual(count, 1)
        self.assertEqual(len(errors), 1)


if __name__ == "__main__":
    unittest.main()
