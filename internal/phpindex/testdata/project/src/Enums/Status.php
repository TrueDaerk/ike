<?php

namespace App\Enums;

use App\Traits\X;

enum Status: string
{
    use X;

    case Active = 'active';
    case Inactive = 'inactive';

    const DEFAULT = self::Active;

    public function label(): string
    {
        return ucfirst($this->value);
    }
}
