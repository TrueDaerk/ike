<?php

// A legacy file without a namespace: the consumer resolves LegacyTrait by
// its short name because no import or namespace qualifies it.
class OldConsumer
{
    use LegacyTrait;

    var $legacyProp;

    function legacy()
    {
        return $this->helper();
    }
}

interface OldContract
{
    public function contract(): void;
}
