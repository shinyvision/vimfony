<?php

declare(strict_types=1);

namespace App;

use Symfony\Component\Routing\RouterInterface as MyAliasedRouter;
use Symfony\Component\Routing\RouterInterface;
use Symfony\Component\Routing\Router;
use Symfony\Component\Routing\Generator\UrlGeneratorInterface;
use Symfony\Component\Routing\Generator\UrlGenerator;
use Symfony\Bundle\FrameworkBundle\Controller\AbstractController;
use App\ThisIsNotARouter;

class ClassWithRouter extends AbstractController
{
    private UrlGenerator $urlGenerator;
    private UrlGeneratorInterface $urlGeneratorInterface;
    private ThisIsNotARouter $notARouter;

    public function __construct(
        private MyAliasedRouter $myAliasedRouter,
        private Router $router,
        private RouterInterface $routerInterface,
        private \Symfony\Component\Routing\RouterInterface $fqnRouter,
    ) {
        $this->urlGenerator = $router;
        $this->urlGeneratorInterface = $routerInterface;
        $this->notARouter = new ThisIsNotARouter();
    }

    public function __invoke()
    {
        $a = $this->myAliasedRouter->generate('a_route', ['some' => 'params']);
        $b = $this->router->generate('a_route', ['some' => 'params']);
        $c = $this->routerInterface->generate('a_route', ['some' => 'params']);
        $d = $this->fqnRouter->generate('a_route', ['some' => 'params']);
        $e = $this->urlGenerator->generate('a_route', ['some' => 'params']);
        $f = $this->urlGeneratorInterface->generate('a_route', ['some' => 'params']);
        $g = $this->notARouter->generate('generating_something_that_is_not_a_route');
        $h = $this->router->generate('a_route', ['unborn_param_name']);
    }
}
