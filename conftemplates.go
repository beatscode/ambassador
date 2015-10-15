package main

var proxyconf = string(`server {
    listen 80;
    server_name {{ .Hostname }};

    try_files $uri $uri/;

    location / {
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $remote_addr;
        proxy_set_header Host $host;
        proxy_pass http://{{ .IP }}:{{ .CurrentPort }};
    }
}`)
var phpserverconf = string(`server {
    listen 80;
    server_name {{ .Hostname }};
    keepalive_timeout   70;
    root {{ .Webrootdirectory }};

    try_files $uri $uri/ /index.php$uri /index.php?$args;

    location / {
    #    index  index.html index.htm index.php $uri;
    }

    location ~* \.(jpg|jpeg|gif|png|html|htm|css|zip|tgz|gz|rar|doc|xls|pdf|ppt|tar|wav|bmp|rtf|swf|flv|txt|xml|docx|xlsx|js)$ {
        try_files $uri $uri/ /index.php$uri =404;
        access_log off;
        expires 30d;
    }

    # unless the request is for a valid file (image, js, css, etc.), send to bootstrap
    if (!-e $request_filename)
    {
        rewrite ^/(.*)$ /index.php?/$1 last;
        break;
    }

    location ~ \.php$ {
       fastcgi_pass   {{ .IP }}:{{ .CurrentPort }};
       fastcgi_index  index.php;
       fastcgi_param  SCRIPT_FILENAME  $document_root$fastcgi_script_name;
       fastcgi_param  PATH_INFO               $fastcgi_path_info;
       include        fastcgi_params;
    }
}`)
