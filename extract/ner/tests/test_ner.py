import pytest

from ner import extract


def test_extract_not_yet_implemented():
    with pytest.raises(NotImplementedError):
        extract("een boom")
