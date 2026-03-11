<?php
namespace App\Entity;

use Doctrine\Common\Collections\Collection;
use Doctrine\ORM\Mapping as ORM;

#[ORM\Entity]
class User
{
    #[ORM\Id]
    #[ORM\Column]
    private int $id;

    #[ORM\Column]
    private string $email;

    #[ORM\ManyToOne(targetEntity: Address::class)]
    private Address $address;

    #[ORM\OneToMany(targetEntity: Address::class, mappedBy: 'user')]
    private Collection $addresses;

    #[ORM\ManyToMany(targetEntity: Channel::class)]
    private Collection $channels;
}
