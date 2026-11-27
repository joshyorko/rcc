/* Measure RCC from a small native parent so the Python launcher's RSS
 * cannot set the child's initial high-water mark on Unix. */
#include <stdio.h>
#include <sys/resource.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

int main(int argc, char **argv) {
    if (argc != 3) return 2;
    struct timespec start, end;
    struct rusage usage;
    int status;
    if (clock_gettime(CLOCK_MONOTONIC, &start)) return 3;
    pid_t pid = fork();
    if (pid < 0) return 4;
    if (pid == 0) {
        execl(argv[1], argv[1], "version", (char *)NULL);
        _exit(127);
    }
    if (wait4(pid, &status, 0, &usage) != pid ||
        clock_gettime(CLOCK_MONOTONIC, &end)) return 5;
    if (!WIFEXITED(status) || WEXITSTATUS(status)) return 6;
    FILE *file = fopen(argv[2], "w");
    if (!file) return 7;
    double elapsed = end.tv_sec - start.tv_sec + (end.tv_nsec - start.tv_nsec) / 1e9;
    if (fprintf(file, "%.9f %ld\n", elapsed, usage.ru_maxrss) < 0) return 8;
    return fclose(file) == 0 ? 0 : 9;
}
