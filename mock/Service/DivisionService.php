<?php
namespace App\Service;

use App\Entity\User;

class DivisionService
{
    private $entityManager;

    public function findByChannel()
    {
        $result = $this->entityManager->getRepository(User::class)
            ->createQueryBuilder('u')
            ->join('u.channels', 'c')
            ->andWhere('c.')
            ->getQuery()
            ->getOneOrNullResult();
    }
}
